package agentbox

import "fmt"

// TransportError means the request could not reach the configured endpoint.
type TransportError struct {
	Message string
}

func (e *TransportError) Error() string { return e.Message }

// SandboxWaitTimeout means WaitUntilRunning exceeded its timeout before the
// sandbox reached the running status.
type SandboxWaitTimeout struct {
	Message string
}

func (e *SandboxWaitTimeout) Error() string { return e.Message }

// SandboxFailed means the sandbox reached a terminal non-running status
// while WaitUntilRunning was polling.
type SandboxFailed struct {
	Status  string
	Message string
}

func (e *SandboxFailed) Error() string { return e.Message }

// ErrorKind identifies which HTTP-status-derived APIError variant occurred.
type ErrorKind string

const (
	ErrorKindGeneric          ErrorKind = "api_error"
	ErrorKindBadRequest       ErrorKind = "bad_request"
	ErrorKindAuthentication   ErrorKind = "authentication"
	ErrorKindPermissionDenied ErrorKind = "permission_denied"
	ErrorKindNotFound         ErrorKind = "not_found"
	ErrorKindConflict         ErrorKind = "conflict"
	ErrorKindUnprocessable    ErrorKind = "unprocessable"
	ErrorKindRateLimit        ErrorKind = "rate_limit"
	ErrorKindServer           ErrorKind = "server_error"
)

// APIError means the server rejected a request. Kind distinguishes the
// specific status-derived variant (see the Is*Error helpers below).
type APIError struct {
	Kind       ErrorKind
	StatusCode int
	Message    string
	Code       string
	Details    map[string]any
}

func (e *APIError) Error() string {
	return fmt.Sprintf("agentbox: %s (status=%d)", e.Message, e.StatusCode)
}

// IsBadRequestError reports whether err is an APIError for HTTP 400.
func IsBadRequestError(err error) bool { return isAPIErrorKind(err, ErrorKindBadRequest) }

// IsAuthenticationError reports whether err is an APIError for HTTP 401.
func IsAuthenticationError(err error) bool { return isAPIErrorKind(err, ErrorKindAuthentication) }

// IsPermissionDeniedError reports whether err is an APIError for HTTP 403.
func IsPermissionDeniedError(err error) bool {
	return isAPIErrorKind(err, ErrorKindPermissionDenied)
}

// IsNotFoundError reports whether err is an APIError for HTTP 404.
func IsNotFoundError(err error) bool { return isAPIErrorKind(err, ErrorKindNotFound) }

// IsConflictError reports whether err is an APIError for HTTP 409.
func IsConflictError(err error) bool { return isAPIErrorKind(err, ErrorKindConflict) }

// IsUnprocessableError reports whether err is an APIError for HTTP 422.
func IsUnprocessableError(err error) bool { return isAPIErrorKind(err, ErrorKindUnprocessable) }

// IsRateLimitError reports whether err is an APIError for HTTP 429.
func IsRateLimitError(err error) bool { return isAPIErrorKind(err, ErrorKindRateLimit) }

// IsServerError reports whether err is an APIError for an HTTP 5xx status.
func IsServerError(err error) bool { return isAPIErrorKind(err, ErrorKindServer) }

func isAPIErrorKind(err error, kind ErrorKind) bool {
	apiErr, ok := err.(*APIError)
	return ok && apiErr.Kind == kind
}

func errorKindForStatus(statusCode int) ErrorKind {
	switch statusCode {
	case 400:
		return ErrorKindBadRequest
	case 401:
		return ErrorKindAuthentication
	case 403:
		return ErrorKindPermissionDenied
	case 404:
		return ErrorKindNotFound
	case 409:
		return ErrorKindConflict
	case 422:
		return ErrorKindUnprocessable
	case 429:
		return ErrorKindRateLimit
	}
	if statusCode >= 500 {
		return ErrorKindServer
	}
	return ErrorKindGeneric
}

// errorFromResponse maps API `{error}` payloads (and message/code variants)
// to an *APIError, mirroring the Python SDK's error_from_response.
func errorFromResponse(statusCode int, payload any) error {
	message := "Request failed"
	var code string
	var details map[string]any

	if m, ok := payload.(map[string]any); ok {
		if v, ok := m["error"].(string); ok && v != "" {
			message = v
		} else if v, ok := m["message"].(string); ok && v != "" {
			message = v
		}
		if v, ok := m["code"].(string); ok {
			code = v
		}
		if v, ok := m["details"].(map[string]any); ok {
			details = v
		}
	}

	return &APIError{
		Kind:       errorKindForStatus(statusCode),
		StatusCode: statusCode,
		Message:    message,
		Code:       code,
		Details:    details,
	}
}
