package agentbox

import (
	"errors"
	"fmt"
)

// TransportError means the request could not reach the configured endpoint.
type TransportError struct {
	Message string
	// Err is the underlying cause, if any. It is reachable via errors.Is and
	// errors.As.
	Err error
}

func (e *TransportError) Error() string { return e.Message }

// Unwrap returns the underlying cause.
func (e *TransportError) Unwrap() error { return e.Err }

func newTransportError(err error) *TransportError {
	return &TransportError{Message: err.Error(), Err: err}
}

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

// SnapshotWaitTimeout means Snapshot.WaitUntilReady exceeded its timeout
// before the snapshot reached the ready status.
type SnapshotWaitTimeout struct {
	Message string
}

func (e *SnapshotWaitTimeout) Error() string { return e.Message }

// SnapshotFailed means the snapshot reached error, deleting or delete_failed
// while Snapshot.WaitUntilReady was polling.
type SnapshotFailed struct {
	Status  string
	Message string
}

func (e *SnapshotFailed) Error() string { return e.Message }

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
	ErrorKindGone             ErrorKind = "gone"
	ErrorKindRateLimit        ErrorKind = "rate_limit"
	ErrorKindNotSupported     ErrorKind = "not_supported"
	ErrorKindServer           ErrorKind = "server_error"

	ErrorKindRequestTooLarge      ErrorKind = "request_too_large"
	ErrorKindUnsupportedMediaType ErrorKind = "unsupported_media_type"
	ErrorKindRangeNotSatisfiable  ErrorKind = "range_not_satisfiable"
	ErrorKindRequestCanceled      ErrorKind = "request_canceled"
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

// IsGoneError reports whether err is an APIError for HTTP 410.
// Build log reads return this when the runtime has reclaimed the artifact
// (build_logs_expired). Stop reading; the logs are gone, including for
// successful builds. This is not a build failure.
func IsGoneError(err error) bool { return isAPIErrorKind(err, ErrorKindGone) }

// IsRateLimitError reports whether err is an APIError for HTTP 429.
func IsRateLimitError(err error) bool { return isAPIErrorKind(err, ErrorKindRateLimit) }

// IsNotSupportedError reports whether err is an APIError for HTTP 501.
// Template build APIs return this for container deployments
// (not_supported_by_runtime). Do not retry it as a transient server error.
func IsNotSupportedError(err error) bool { return isAPIErrorKind(err, ErrorKindNotSupported) }

// IsServerError reports whether err is an APIError for an HTTP 5xx status
// other than 501.
func IsServerError(err error) bool { return isAPIErrorKind(err, ErrorKindServer) }

// IsRequestTooLargeError reports whether err is an APIError for HTTP 413.
func IsRequestTooLargeError(err error) bool { return isAPIErrorKind(err, ErrorKindRequestTooLarge) }

// IsUnsupportedMediaTypeError reports whether err is an APIError for HTTP 415.
func IsUnsupportedMediaTypeError(err error) bool {
	return isAPIErrorKind(err, ErrorKindUnsupportedMediaType)
}

// IsRangeNotSatisfiableError reports whether err is an APIError for HTTP 416.
func IsRangeNotSatisfiableError(err error) bool {
	return isAPIErrorKind(err, ErrorKindRangeNotSatisfiable)
}

// IsRequestCanceledError reports whether err is an APIError for HTTP 499.
func IsRequestCanceledError(err error) bool { return isAPIErrorKind(err, ErrorKindRequestCanceled) }

// IsInsufficientStorageError reports whether err is an APIError for HTTP 507.
// It keeps ErrorKindServer, so IsServerError is also true for it.
func IsInsufficientStorageError(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == 507
}

func isAPIErrorKind(err error, kind ErrorKind) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.Kind == kind
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
	case 410:
		return ErrorKindGone
	case 413:
		return ErrorKindRequestTooLarge
	case 415:
		return ErrorKindUnsupportedMediaType
	case 416:
		return ErrorKindRangeNotSatisfiable
	case 422:
		return ErrorKindUnprocessable
	case 429:
		return ErrorKindRateLimit
	case 499:
		return ErrorKindRequestCanceled
	case 501:
		return ErrorKindNotSupported
	}
	if statusCode >= 500 {
		return ErrorKindServer
	}
	return ErrorKindGeneric
}

// errorFromBody maps an error response body: a JSON body goes through
// errorFromResponse, anything else becomes the message as raw text.
func errorFromBody(statusCode int, body []byte) error {
	if payload := jsonLoads(body); payload != nil {
		return errorFromResponse(statusCode, payload)
	}
	return errorFromResponse(statusCode, string(body))
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
