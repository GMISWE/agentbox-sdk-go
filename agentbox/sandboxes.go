package agentbox

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
)

// Execution is a sandbox command execution.
type Execution struct {
	Data     map[string]any
	Accepted bool
}

// ID is the execution identifier.
func (e *Execution) ID() string {
	if v := stringField(e.Data, "execution_id"); v != "" {
		return v
	}
	return stringField(e.Data, "id")
}

// Status is the current execution status.
func (e *Execution) Status() string { return stringField(e.Data, "status") }

// ExitCode is the process exit code when complete, or nil.
func (e *Execution) ExitCode() *int {
	switch v := e.Data["exit_code"].(type) {
	case float64:
		i := int(v)
		return &i
	case int:
		return &v
	}
	return nil
}

// FileDownload is a downloaded sandbox file and its response metadata.
type FileDownload struct {
	Content  []byte
	Filename string
	Header   http.Header
}

// MetricSeries is one metrics timeseries (Kind + Points).
type MetricSeries struct {
	Kind        string
	Unit        string
	EmptyReason string
	Points      []any
	Data        map[string]any
}

// MetricsBatch is the bundled GET /tasks/{id}/metrics payload.
type MetricsBatch struct {
	Results []*MetricSeries
	Data    map[string]any
}

func metricSeriesFromPayload(payload map[string]any) *MetricSeries {
	return &MetricSeries{
		Kind:        stringField(payload, "kind"),
		Unit:        stringField(payload, "unit"),
		EmptyReason: stringField(payload, "empty_reason"),
		Points:      sliceField(payload, "points"),
		Data:        payload,
	}
}

// Sandbox represents a launched AgentBox container.
type Sandbox struct {
	client *Client
	Data   map[string]any
}

// ID is the Sandbox ID.
func (s *Sandbox) ID() string { return stringField(s.Data, "id") }

// Status is the current lifecycle status.
func (s *Sandbox) Status() string {
	if v := stringField(s.Data, "task_status"); v != "" {
		return v
	}
	return stringField(s.Data, "status")
}

// EndpointURL is the sandbox's endpoint URL, or "" if unset (including the
// running-but-not-yet-assigned case).
func (s *Sandbox) EndpointURL() string { return stringField(s.Data, "endpoint_url") }

// AgentID is the owning Agent's ID.
func (s *Sandbox) AgentID() string { return stringField(s.Data, "deployment_id") }

// AgentSlug is the owning Agent's slug.
func (s *Sandbox) AgentSlug() string { return stringField(s.Data, "deployment_slug") }

// DisplayName is the Sandbox's display name.
func (s *Sandbox) DisplayName() string { return stringField(s.Data, "display_name") }

// InstanceType is the billing SKU, not the product object name.
func (s *Sandbox) InstanceType() string { return stringField(s.Data, "instance_type") }

// IdcName is the data center identifier.
func (s *Sandbox) IdcName() string { return stringField(s.Data, "idc_name") }

// StatusStale reports whether the cached status may be out of date.
func (s *Sandbox) StatusStale() bool {
	v, _ := s.Data["status_stale"].(bool)
	return v
}

// LastError is the last error reported for the Sandbox, if any.
func (s *Sandbox) LastError() string { return stringField(s.Data, "last_error") }

// Runtime is the runtime name, e.g. "sandbox".
func (s *Sandbox) Runtime() string { return stringField(s.Data, "runtime") }

// Capabilities are the server-derived enabled surfaces for this Sandbox.
func (s *Sandbox) Capabilities() map[string]any {
	v, _ := s.Data["capabilities"].(map[string]any)
	return v
}

// ExpiresAt is the Sandbox expiry timestamp, if any.
func (s *Sandbox) ExpiresAt() string { return stringField(s.Data, "expires_at") }

// Refresh reloads the Sandbox from the API.
func (s *Sandbox) Refresh(ctx context.Context) error {
	updated, err := s.client.Sandboxes.Get(ctx, s.ID())
	if err != nil {
		return err
	}
	s.Data = updated.Data
	return nil
}

// Logs returns the Sandbox's log snapshot.
func (s *Sandbox) Logs(ctx context.Context) (string, error) {
	return s.client.Sandboxes.Logs(ctx, s.ID())
}

// MetricsParams configures Sandbox.Metrics / SandboxCollection.Metrics.
type MetricsParams struct {
	Start int64
	End   int64
	Kinds []string // omit for all 8 charts
	Step  *int
}

// Metrics returns a bundle of metric timeseries for the Sandbox.
func (s *Sandbox) Metrics(ctx context.Context, params MetricsParams) (*MetricsBatch, error) {
	return s.client.Sandboxes.Metrics(ctx, s.ID(), params)
}

// MetricsTimeseriesParams configures Sandbox.MetricsTimeseries /
// SandboxCollection.MetricsTimeseries.
type MetricsTimeseriesParams struct {
	Kind  string
	Start int64
	End   int64
	Step  *int
}

// MetricsTimeseries returns a single metric timeseries for the Sandbox.
func (s *Sandbox) MetricsTimeseries(ctx context.Context, params MetricsTimeseriesParams) (*MetricSeries, error) {
	return s.client.Sandboxes.MetricsTimeseries(ctx, s.ID(), params)
}

// Stream opens a Server-Sent Events stream of Sandbox status/log events.
func (s *Sandbox) Stream(ctx context.Context) (*Stream, error) {
	return s.client.Sandboxes.Stream(ctx, s.ID())
}

// Execute runs a command in the Sandbox. wait=true (Execute's default via
// ExecuteParams.Wait left nil) blocks until the command completes.
func (s *Sandbox) Execute(ctx context.Context, command string, params ExecuteParams) (*Execution, error) {
	return s.client.Sandboxes.Execute(ctx, s.ID(), command, params)
}

// GetExecution retrieves the latest state of an execution.
func (s *Sandbox) GetExecution(ctx context.Context, executionID string) (*Execution, error) {
	return s.client.Sandboxes.GetExecution(ctx, s.ID(), executionID)
}

// CancelExecution requests cancellation of an execution.
func (s *Sandbox) CancelExecution(ctx context.Context, executionID string) (*Execution, error) {
	return s.client.Sandboxes.CancelExecution(ctx, s.ID(), executionID)
}

// UploadFile uploads a file to an absolute Sandbox path.
func (s *Sandbox) UploadFile(ctx context.Context, sandboxPath string, filename string, content io.Reader) ([]map[string]any, error) {
	return s.client.Sandboxes.UploadFile(ctx, s.ID(), sandboxPath, filename, content)
}

// DownloadFile downloads a file from an absolute Sandbox path.
func (s *Sandbox) DownloadFile(ctx context.Context, sandboxPath string) (*FileDownload, error) {
	return s.client.Sandboxes.DownloadFile(ctx, s.ID(), sandboxPath)
}

// Shell opens an interactive WebSocket shell connection.
func (s *Sandbox) Shell(ctx context.Context) (ShellConn, error) {
	return s.client.Sandboxes.Shell(ctx, s.ID())
}

// Delete deletes the Sandbox. This is one-way; the sandbox cannot be
// restarted.
func (s *Sandbox) Delete(ctx context.Context) error {
	return s.client.Sandboxes.Delete(ctx, s.ID())
}

// WaitUntilRunningParams configures Sandbox.WaitUntilRunning.
type WaitUntilRunningParams struct {
	Timeout      time.Duration // default DefaultWaitTimeout
	PollInterval time.Duration // default 2s
}

// WaitUntilRunning polls until the Sandbox's status becomes "running". It
// returns *SandboxFailed if the status reaches a terminal non-running state
// (error, deleted, stopped, stopping), or *SandboxWaitTimeout if the timeout
// elapses first.
func (s *Sandbox) WaitUntilRunning(ctx context.Context, params WaitUntilRunningParams) error {
	timeout := params.Timeout
	if timeout == 0 {
		timeout = DefaultWaitTimeout
	}
	pollInterval := params.PollInterval
	if pollInterval == 0 {
		pollInterval = 2 * time.Second
	}

	deadline := time.Now().Add(timeout)
	if err := s.Refresh(ctx); err != nil {
		return err
	}
	for {
		status := s.Status()
		if status == runningStatus {
			return nil
		}
		if waitFailureStatuses[status] {
			message := s.LastError()
			if message == "" {
				message = fmt.Sprintf("sandbox %s reached status %s", s.ID(), status)
			}
			return &SandboxFailed{Status: status, Message: message}
		}
		if time.Now().After(deadline) || time.Now().Equal(deadline) {
			return &SandboxWaitTimeout{
				Message: fmt.Sprintf("sandbox %s still %q after %s", s.ID(), status, timeout),
			}
		}
		s.client.sleep(pollInterval)
		if err := s.Refresh(ctx); err != nil {
			return err
		}
	}
}

// SandboxCollection provides operations on Sandboxes (client.Sandboxes).
type SandboxCollection struct {
	client *Client
}

func (sc *SandboxCollection) launch(ctx context.Context, slug string, params LaunchParams, idcName, templateID string) (*Sandbox, error) {
	if idcName == "" {
		return nil, errors.New("agentbox: idc_name is required; pass the agent's Idc field (an idcId, not a region name)")
	}

	body := compact(map[string]any{
		"idc_name":         idcName,
		"instance_type":    params.InstanceType,
		"env":              envOrNil(params.Env),
		"display_name":     nonEmptyString(params.DisplayName),
		"assign_public_ip": params.AssignPublicIP,
		"ports":            portsOrNil(params.Ports),
		"storages":         storagesOrNil(params.Storages),
		"template_id":      nonEmptyString(templateID),
	})

	var payload struct {
		TaskID      string `json:"task_id"`
		ContainerID string `json:"container_id"`
		Status      string `json:"status"`
	}
	if err := sc.client.request(ctx, http.MethodPost, fmt.Sprintf("/deployments/%s/tasks", slug), nil, body, nil, &payload); err != nil {
		return nil, err
	}

	return &Sandbox{
		client: sc.client,
		Data: map[string]any{
			"id":              payload.TaskID,
			"container_id":    payload.ContainerID,
			"task_status":     payload.Status,
			"deployment_slug": slug,
			"idc_name":        idcName,
			"instance_type":   params.InstanceType,
			"display_name":    params.DisplayName,
		},
	}, nil
}

// Launch starts a Sandbox from an Agent slug. Unlike Agent.Launch, idcName
// is required here.
func (sc *SandboxCollection) Launch(ctx context.Context, slug string, params LaunchParams) (*Sandbox, error) {
	return sc.launch(ctx, slug, params, params.IdcName, "")
}

// SandboxListParams configures SandboxCollection.List.
type SandboxListParams struct {
	AgentID  string
	Status   []string // if omitted, the API excludes "stopped" and "deleted"
	Page     int
	PageSize int
}

// List returns a page of Sandboxes.
func (sc *SandboxCollection) List(ctx context.Context, params SandboxListParams) (*Page[*Sandbox], error) {
	page := params.Page
	if page == 0 {
		page = 1
	}
	pageSize := params.PageSize
	if pageSize == 0 {
		pageSize = 20
	}

	var statusValue string
	if len(params.Status) > 0 {
		statusValue = strings.Join(params.Status, ",")
	}

	var payload struct {
		Items    []map[string]any `json:"items"`
		Total    int              `json:"total"`
		Page     int              `json:"page"`
		PageSize int              `json:"page_size"`
	}
	err := sc.client.request(ctx, http.MethodGet, "/tasks", compact(map[string]any{
		"deployment_id": nonEmptyString(params.AgentID),
		"status":        nonEmptyString(statusValue),
		"page":          page,
		"page_size":     pageSize,
	}), nil, nil, &payload)
	if err != nil {
		return nil, err
	}
	items := make([]*Sandbox, 0, len(payload.Items))
	for _, item := range payload.Items {
		items = append(items, &Sandbox{client: sc.client, Data: item})
	}
	return &Page[*Sandbox]{
		Items:    items,
		Total:    payload.Total,
		Page:     orDefault(payload.Page, page),
		PageSize: orDefault(payload.PageSize, pageSize),
	}, nil
}

// Get retrieves a Sandbox by ID.
func (sc *SandboxCollection) Get(ctx context.Context, sandboxID string) (*Sandbox, error) {
	var payload map[string]any
	if err := sc.client.request(ctx, http.MethodGet, fmt.Sprintf("/tasks/%s", sandboxID), nil, nil, nil, &payload); err != nil {
		return nil, err
	}
	return &Sandbox{client: sc.client, Data: payload}, nil
}

// Delete deletes a Sandbox by ID. This is one-way; the sandbox cannot be
// restarted.
func (sc *SandboxCollection) Delete(ctx context.Context, sandboxID string) error {
	return sc.client.request(ctx, http.MethodDelete, fmt.Sprintf("/tasks/%s", sandboxID), nil, nil, nil, nil)
}

// Logs returns the Sandbox's log snapshot, or "" if unavailable.
func (sc *SandboxCollection) Logs(ctx context.Context, sandboxID string) (string, error) {
	var payload map[string]any
	if err := sc.client.request(ctx, http.MethodGet, fmt.Sprintf("/tasks/%s/logs", sandboxID), nil, nil, nil, &payload); err != nil {
		return "", err
	}
	return stringField(payload, "logs"), nil
}

// Metrics returns a bundle of metric timeseries for a Sandbox. Start/End are
// unix seconds. Step defaults on the API to 60s (min 5, max 3600).
func (sc *SandboxCollection) Metrics(ctx context.Context, sandboxID string, params MetricsParams) (*MetricsBatch, error) {
	var payload map[string]any
	err := sc.client.request(ctx, http.MethodGet, fmt.Sprintf("/tasks/%s/metrics", sandboxID), compact(map[string]any{
		"kinds": nonEmptyString(kindsQuery(params.Kinds)),
		"start": params.Start,
		"end":   params.End,
		"step":  stepValue(params.Step),
	}), nil, nil, &payload)
	if err != nil {
		return nil, err
	}
	rawResults, _ := payload["results"].([]any)
	results := make([]*MetricSeries, 0, len(rawResults))
	for _, item := range rawResults {
		if m, ok := item.(map[string]any); ok {
			results = append(results, metricSeriesFromPayload(m))
		}
	}
	return &MetricsBatch{Results: results, Data: payload}, nil
}

// MetricsTimeseries returns a single metric timeseries for a Sandbox. Kind
// is one of gpu_util, gpu_mem, cpu, memory, disk_read, disk_write, net_rx,
// net_tx.
func (sc *SandboxCollection) MetricsTimeseries(ctx context.Context, sandboxID string, params MetricsTimeseriesParams) (*MetricSeries, error) {
	var payload map[string]any
	err := sc.client.request(ctx, http.MethodGet, fmt.Sprintf("/tasks/%s/metrics/timeseries", sandboxID), compact(map[string]any{
		"kind":  params.Kind,
		"start": params.Start,
		"end":   params.End,
		"step":  stepValue(params.Step),
	}), nil, nil, &payload)
	if err != nil {
		return nil, err
	}
	return metricSeriesFromPayload(payload), nil
}

// Stream opens a Server-Sent Events stream of Sandbox status/log events.
// 4xx/5xx responses return the same *APIError types as other calls.
func (sc *SandboxCollection) Stream(ctx context.Context, sandboxID string) (*Stream, error) {
	return sc.client.stream(ctx, fmt.Sprintf("/tasks/%s/stream", sandboxID))
}

// ExecuteParams configures SandboxCollection.Execute.
type ExecuteParams struct {
	Wait               *bool // defaults to true
	WaitTimeoutSeconds *int  // ignored when Wait is false
}

// Execute runs a command in a Sandbox. A completed command returns
// Accepted=false; an accepted asynchronous command returns Accepted=true.
func (sc *SandboxCollection) Execute(ctx context.Context, sandboxID, command string, params ExecuteParams) (*Execution, error) {
	wait := true
	if params.Wait != nil {
		wait = *params.Wait
	}

	body := map[string]any{
		"command": command,
		"wait":    wait,
	}
	if wait && params.WaitTimeoutSeconds != nil {
		body["wait_timeout_seconds"] = *params.WaitTimeoutSeconds
	}

	resp, err := sc.client.requestRaw(ctx, http.MethodPost, fmt.Sprintf("/tasks/%s/executions", sandboxID), nil, body, nil)
	if err != nil {
		return nil, err
	}
	data, _ := jsonLoads(resp.Body).(map[string]any)
	if data == nil {
		data = map[string]any{}
	}
	return &Execution{Data: data, Accepted: resp.StatusCode == http.StatusAccepted}, nil
}

// GetExecution retrieves the latest state of an execution.
func (sc *SandboxCollection) GetExecution(ctx context.Context, sandboxID, executionID string) (*Execution, error) {
	var payload map[string]any
	err := sc.client.request(ctx, http.MethodGet, fmt.Sprintf("/tasks/%s/executions/%s", sandboxID, executionID), nil, nil, nil, &payload)
	if err != nil {
		return nil, err
	}
	return &Execution{Data: payload}, nil
}

// CancelExecution requests cancellation of an execution.
func (sc *SandboxCollection) CancelExecution(ctx context.Context, sandboxID, executionID string) (*Execution, error) {
	resp, err := sc.client.requestRaw(ctx, http.MethodPost, fmt.Sprintf("/tasks/%s/executions/%s/cancel", sandboxID, executionID), nil, nil, nil)
	if err != nil {
		return nil, err
	}
	data, _ := jsonLoads(resp.Body).(map[string]any)
	if data == nil {
		data = map[string]any{}
	}
	return &Execution{Data: data, Accepted: resp.StatusCode == http.StatusAccepted}, nil
}

// UploadFile uploads content to an absolute Sandbox path. filename is used
// as the multipart form filename (e.g. path.Base(sandboxPath)).
func (sc *SandboxCollection) UploadFile(ctx context.Context, sandboxID, sandboxPath, filename string, content io.Reader) ([]map[string]any, error) {
	if err := validateSandboxPath(sandboxPath); err != nil {
		return nil, err
	}
	if filename == "" {
		filename = path.Base(sandboxPath)
	}

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		return nil, fmt.Errorf("agentbox: build upload body: %w", err)
	}
	if _, err := io.Copy(part, content); err != nil {
		return nil, fmt.Errorf("agentbox: read upload content: %w", err)
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("agentbox: build upload body: %w", err)
	}

	headers := http.Header{}
	headers.Set("Content-Type", writer.FormDataContentType())

	var payload []map[string]any
	err = sc.client.request(ctx, http.MethodPost, fmt.Sprintf("/tasks/%s/files", sandboxID),
		map[string]any{"path": sandboxPath}, buf.Bytes(), headers, &payload)
	if err != nil {
		return nil, err
	}
	return payload, nil
}

// DownloadFile downloads a file from an absolute Sandbox path.
func (sc *SandboxCollection) DownloadFile(ctx context.Context, sandboxID, sandboxPath string) (*FileDownload, error) {
	if err := validateSandboxPath(sandboxPath); err != nil {
		return nil, err
	}
	resp, err := sc.client.requestRaw(ctx, http.MethodGet, fmt.Sprintf("/tasks/%s/files", sandboxID),
		map[string]any{"path": sandboxPath}, nil, nil)
	if err != nil {
		return nil, err
	}

	filename := ""
	for part := range strings.SplitSeq(resp.Header.Get("Content-Disposition"), ";") {
		key, value, found := strings.Cut(strings.TrimSpace(part), "=")
		if found && strings.EqualFold(strings.TrimSpace(key), "filename") {
			filename = strings.Trim(strings.TrimSpace(value), `"`)
			break
		}
	}
	if filename == "" {
		filename = path.Base(sandboxPath)
	}

	return &FileDownload{Content: resp.Body, Filename: filename, Header: resp.Header}, nil
}

// Shell opens an interactive WebSocket shell connection for a Sandbox.
func (sc *SandboxCollection) Shell(ctx context.Context, sandboxID string) (ShellConn, error) {
	fullURL := joinURL(sc.client.serviceURL, fmt.Sprintf("/tasks/%s/shell", sandboxID))
	parsed, err := url.Parse(fullURL)
	if err != nil {
		return nil, fmt.Errorf("agentbox: parse shell url: %w", err)
	}
	scheme := "ws"
	if parsed.Scheme == "https" {
		scheme = "wss"
	}
	parsed.Scheme = scheme
	return sc.client.shellDialer(ctx, parsed.String(), sc.client.authHeaders(nil))
}

func validateSandboxPath(sandboxPath string) error {
	decoded, err := url.PathUnescape(sandboxPath)
	if err != nil {
		return fmt.Errorf("agentbox: invalid path encoding: %w", err)
	}
	if !strings.HasPrefix(decoded, "/") {
		return errors.New("agentbox: path must be absolute")
	}
	for segment := range strings.SplitSeq(decoded, "/") {
		if segment == "." || segment == ".." {
			return errors.New("agentbox: path must not contain . or .. segments")
		}
	}
	return nil
}

func kindsQuery(kinds []string) string {
	if len(kinds) == 0 {
		return ""
	}
	return strings.Join(kinds, ",")
}

func stepValue(step *int) any {
	if step == nil {
		return nil
	}
	return *step
}

func nonEmptyString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func storagesOrNil(storages []map[string]any) any {
	if storages == nil {
		return nil
	}
	return storages
}
