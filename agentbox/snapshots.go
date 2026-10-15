package agentbox

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"
)

const snapshotReadyStatus = "ready"

var snapshotWaitFailureStatuses = map[string]bool{
	"error":         true,
	"deleting":      true,
	"delete_failed": true,
}

// Snapshot is a durable copy of a sandbox. New sandboxes of the same Agent
// can launch from a ready snapshot via LaunchParams.SnapshotID.
type Snapshot struct {
	client    *Client
	agentSlug string
	Data      map[string]any
}

// ID is the Snapshot ID.
func (s *Snapshot) ID() string { return stringField(s.Data, "id") }

// Name is the Snapshot's name.
func (s *Snapshot) Name() string { return stringField(s.Data, "name") }

// Status is one of creating, ready, error, deleting or delete_failed.
func (s *Snapshot) Status() string { return stringField(s.Data, "status") }

// StatusReason explains an error or delete_failed status, if any.
func (s *Snapshot) StatusReason() string { return stringField(s.Data, "status_reason") }

// FailureCode is the upstream failure code, if any.
func (s *Snapshot) FailureCode() string { return stringField(s.Data, "failure_code") }

// AgentID is the owning Agent's ID.
func (s *Snapshot) AgentID() string { return stringField(s.Data, "deployment_id") }

// AgentSlug is the owning Agent's slug, or "" when the Snapshot was not
// obtained through an Agent or a Sandbox that knows it.
func (s *Snapshot) AgentSlug() string { return s.agentSlug }

// SourceSandboxID is the ID of the Sandbox the snapshot was saved from.
func (s *Snapshot) SourceSandboxID() string { return stringField(s.Data, "source_task_id") }

// IdcName is the data center identifier. Sandboxes launched from the
// snapshot run in this data center.
func (s *Snapshot) IdcName() string { return stringField(s.Data, "idc_name") }

// ProductName is the sizing product, e.g. "SMALL".
func (s *Snapshot) ProductName() string { return stringField(s.Data, "product_name") }

// VCPU is the number of vCPUs inherited by sandboxes launched from it.
func (s *Snapshot) VCPU() int { return intField(s.Data, "vcpu") }

// RAMMB is the memory in MiB inherited by sandboxes launched from it.
func (s *Snapshot) RAMMB() int { return intField(s.Data, "ram_mb") }

// DiskSizeMB is the disk size in MiB.
func (s *Snapshot) DiskSizeMB() int { return intField(s.Data, "disk_size_mb") }

// Architecture is the CPU architecture, e.g. "x86_64".
func (s *Snapshot) Architecture() string { return stringField(s.Data, "architecture") }

// CreatedBy is the ID of the user who created the snapshot.
func (s *Snapshot) CreatedBy() string { return stringField(s.Data, "created_by") }

// CreatedAt is the creation timestamp.
func (s *Snapshot) CreatedAt() string { return stringField(s.Data, "created_at") }

// ReadyAt is when the snapshot became ready, if it has.
func (s *Snapshot) ReadyAt() string { return stringField(s.Data, "ready_at") }

// Refresh reloads the Snapshot from its Agent's snapshot list. It needs
// AgentSlug. A snapshot that finished deleting is no longer listed; Refresh
// then returns an *APIError for which IsNotFoundError is true.
func (s *Snapshot) Refresh(ctx context.Context) error {
	if s.agentSlug == "" {
		return errors.New("agentbox: snapshot agent slug is unknown; list snapshots with Agent.ListSnapshots")
	}
	snapshots, err := s.client.Agents.ListSnapshots(ctx, s.agentSlug)
	if err != nil {
		return err
	}
	for _, snapshot := range snapshots {
		if snapshot.ID() == s.ID() {
			s.Data = snapshot.Data
			return nil
		}
	}
	return &APIError{
		Kind:       ErrorKindNotFound,
		StatusCode: http.StatusNotFound,
		Message:    fmt.Sprintf("snapshot %s not found", s.ID()),
		Code:       "snapshot_not_found",
	}
}

// WaitUntilReadyParams configures Snapshot.WaitUntilReady.
type WaitUntilReadyParams struct {
	Timeout      time.Duration // default DefaultWaitTimeout
	PollInterval time.Duration // default 2s
}

// WaitUntilReady polls until the Snapshot's status becomes "ready". It
// returns *SnapshotFailed if the status reaches error, deleting or
// delete_failed, or *SnapshotWaitTimeout if the timeout elapses first. It
// needs AgentSlug, like Refresh.
func (s *Snapshot) WaitUntilReady(ctx context.Context, params WaitUntilReadyParams) error {
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
		if status == snapshotReadyStatus {
			return nil
		}
		if snapshotWaitFailureStatuses[status] {
			message := s.StatusReason()
			if message == "" {
				message = fmt.Sprintf("snapshot %s reached status %s", s.ID(), status)
			}
			return &SnapshotFailed{Status: status, Message: message}
		}
		if !time.Now().Before(deadline) {
			return &SnapshotWaitTimeout{
				Message: fmt.Sprintf("snapshot %s still %q after %s", s.ID(), status, timeout),
			}
		}
		s.client.sleep(pollInterval)
		if err := s.Refresh(ctx); err != nil {
			return err
		}
	}
}

// Rename renames the Snapshot. name is 1..255 characters. Only a settled
// snapshot (ready or error) can be renamed. On success Data is replaced with
// the response.
func (s *Snapshot) Rename(ctx context.Context, name string) error {
	updated, err := s.client.Snapshots.Rename(ctx, s.ID(), name)
	if err != nil {
		return err
	}
	s.Data = updated.Data
	return nil
}

// Delete deletes the Snapshot. Deletion is asynchronous; on success Status
// is the upstream status at acceptance, usually "deleting".
func (s *Snapshot) Delete(ctx context.Context) error {
	deletion, err := s.client.Snapshots.Delete(ctx, s.ID())
	if err != nil {
		return err
	}
	if s.Data == nil {
		s.Data = map[string]any{}
	}
	if deletion.Status != "" {
		s.Data["status"] = deletion.Status
	}
	return nil
}

// CreateSnapshotParams configures Sandbox.CreateSnapshot. Name empty lets
// the API choose "<sandbox display name or id>-<UTC yyyymmdd-hhmmss>"; a
// non-empty Name is at most 255 characters.
type CreateSnapshotParams struct {
	Name string
}

// SnapshotDeletion is the acceptance result of SnapshotCollection.Delete.
// Status is the upstream status at acceptance, usually "deleting".
type SnapshotDeletion struct {
	SnapshotID string
	Status     string
}

// SnapshotCollection provides operations on Snapshots by ID
// (client.Snapshots). Create a snapshot with Sandbox.CreateSnapshot and list
// them with Agent.ListSnapshots.
type SnapshotCollection struct {
	client *Client
}

// Rename renames a snapshot. See Snapshot.Rename.
//
// HTTP 400 invalid_name means the name is empty, over 255 characters or
// rejected upstream. HTTP 403 means the caller is neither the snapshot's
// creator nor an org owner. HTTP 409 snapshot_not_settled means the snapshot
// is still creating or is being deleted.
func (sc *SnapshotCollection) Rename(ctx context.Context, snapshotID, name string) (*Snapshot, error) {
	var payload struct {
		Snapshot map[string]any `json:"snapshot"`
	}
	err := sc.client.request(ctx, http.MethodPatch, fmt.Sprintf("/snapshots/%s", snapshotID), nil, map[string]any{
		"name": name,
	}, nil, &payload)
	if err != nil {
		return nil, err
	}
	return &Snapshot{client: sc.client, Data: payload.Snapshot}, nil
}

// Delete deletes a snapshot. See Snapshot.Delete.
//
// HTTP 202 means the deletion was accepted; the snapshot shows "deleting" in
// Agent.ListSnapshots until upstream finishes, then disappears, or shows
// "delete_failed" (call Delete again to retry). No new sandbox can launch
// from it once deletion is accepted. HTTP 409 snapshot_conflict means
// upstream refused it.
func (sc *SnapshotCollection) Delete(ctx context.Context, snapshotID string) (*SnapshotDeletion, error) {
	var payload struct {
		SnapshotID string `json:"snapshot_id"`
		Status     string `json:"status"`
	}
	err := sc.client.request(ctx, http.MethodDelete, fmt.Sprintf("/snapshots/%s", snapshotID), nil, nil, nil, &payload)
	if err != nil {
		return nil, err
	}
	return &SnapshotDeletion{SnapshotID: payload.SnapshotID, Status: payload.Status}, nil
}

// CreateSnapshot saves a running Sandbox as a snapshot. See
// Sandbox.CreateSnapshot. The returned Snapshot has no AgentSlug; use
// Sandbox.CreateSnapshot or Agent.ListSnapshots to poll it.
//
// HTTP 201 means the snapshot was created with status "creating". HTTP 400
// invalid_name means the name is over 255 characters. HTTP 403 means the
// caller is neither the sandbox's creator nor an org owner. HTTP 409
// task_not_running means the sandbox does not exist yet; snapshot_conflict
// means upstream refused it, e.g. the sandbox is not running. HTTP 422
// sandbox_only_operation means the task is not a sandbox.
func (sc *SandboxCollection) CreateSnapshot(ctx context.Context, sandboxID string, params CreateSnapshotParams) (*Snapshot, error) {
	var body any
	if params.Name != "" {
		body = map[string]any{"name": params.Name}
	}
	var payload struct {
		Snapshot map[string]any `json:"snapshot"`
	}
	err := sc.client.request(ctx, http.MethodPost, fmt.Sprintf("/tasks/%s/snapshots", sandboxID), nil, body, nil, &payload)
	if err != nil {
		return nil, err
	}
	return &Snapshot{client: sc.client, Data: payload.Snapshot}, nil
}

// CreateSnapshot saves this running Sandbox as a durable snapshot that new
// sandboxes of the same Agent can launch from. Creation is asynchronous: the
// snapshot starts "creating". Poll with Snapshot.WaitUntilReady.
func (s *Sandbox) CreateSnapshot(ctx context.Context, params CreateSnapshotParams) (*Snapshot, error) {
	snapshot, err := s.client.Sandboxes.CreateSnapshot(ctx, s.ID(), params)
	if err != nil {
		return nil, err
	}
	snapshot.agentSlug = s.AgentSlug()
	return snapshot, nil
}

// ListSnapshots lists a sandbox Agent's snapshots, newest first. Status and
// sizing are read live from upstream; a snapshot that finished deleting is
// not listed.
//
// HTTP 422 sandbox_only_operation means the Agent is not a sandbox runtime.
func (a *AgentCollection) ListSnapshots(ctx context.Context, slug string) ([]*Snapshot, error) {
	var payload struct {
		Snapshots []map[string]any `json:"snapshots"`
	}
	err := a.client.request(ctx, http.MethodGet, fmt.Sprintf("/deployments/%s/snapshots", slug), nil, nil, nil, &payload)
	if err != nil {
		return nil, err
	}
	items := make([]*Snapshot, 0, len(payload.Snapshots))
	for _, item := range payload.Snapshots {
		items = append(items, &Snapshot{client: a.client, agentSlug: slug, Data: item})
	}
	return items, nil
}

// ListSnapshots lists this Agent's snapshots, newest first. See
// AgentCollection.ListSnapshots.
func (a *Agent) ListSnapshots(ctx context.Context) ([]*Snapshot, error) {
	return a.client.Agents.ListSnapshots(ctx, a.Slug())
}
