package agentbox

import (
	"context"
	"fmt"
	"net/http"
)

// Agent represents a registered AgentBox template. Registering an Agent
// does not start a container; call Launch to start a Sandbox.
type Agent struct {
	client *Client
	Data   map[string]any
}

// ID is the Agent ID.
func (a *Agent) ID() string { return stringField(a.Data, "id") }

// Slug is the stable Agent identifier.
func (a *Agent) Slug() string { return stringField(a.Data, "slug") }

// Title is the Agent's display name.
func (a *Agent) Title() string { return stringField(a.Data, "title") }

// Idc is the Agent's data center identifier.
func (a *Agent) Idc() string { return stringField(a.Data, "idc") }

// ImageURL is the Agent's container image.
func (a *Agent) ImageURL() string { return stringField(a.Data, "image_url") }

// Runtime is the Agent's selected runtime ("container" or "sandbox").
func (a *Agent) Runtime() string { return stringField(a.Data, "runtime") }

// TemplateBuildStatus is the Sandbox image-build status, if any.
func (a *Agent) TemplateBuildStatus() string { return stringField(a.Data, "template_build_status") }

// TemplateBuildError is the Sandbox image-build error, if any.
func (a *Agent) TemplateBuildError() string { return stringField(a.Data, "template_build_error") }

// Env is a copy of the Agent's environment-variable definitions.
func (a *Agent) Env() []any { return sliceField(a.Data, "env") }

// Revision is the Agent's revision number.
func (a *Agent) Revision() int { return intField(a.Data, "revision") }

// GeneratedAPIKey is the plaintext MaaS key, present only on Create (and
// rare patch retries).
func (a *Agent) GeneratedAPIKey() string { return stringField(a.Data, "generated_api_key") }

// Launchable reports whether the Agent can be launched.
func (a *Agent) Launchable() bool {
	missing, _ := a.Data["upstream_template_missing"].(bool)
	return !missing
}

// Refresh reloads the Agent from the API.
func (a *Agent) Refresh(ctx context.Context) error {
	updated, err := a.client.Agents.Get(ctx, a.Slug())
	if err != nil {
		return err
	}
	a.Data = updated.Data
	return nil
}

// AgentUpdateFields is a PATCH passthrough for Agent.Update /
// AgentCollection.Update. Only send fields the API accepts; locked fields
// return HTTP 422.
type AgentUpdateFields map[string]any

// Update applies a partial update to the Agent.
func (a *Agent) Update(ctx context.Context, fields AgentUpdateFields) error {
	updated, err := a.client.Agents.Update(ctx, a.Slug(), fields)
	if err != nil {
		return err
	}
	a.Data = updated.Data
	return nil
}

// Delete deletes the Agent. It does not stop or delete remaining sandboxes
// of that Agent.
func (a *Agent) Delete(ctx context.Context) error {
	return a.client.Agents.Delete(ctx, a.Slug())
}

// LaunchParams configures Agent.Launch / SandboxCollection.Launch.
type LaunchParams struct {
	InstanceType   string
	IdcName        string // defaults to the Agent's Idc when launched via Agent.Launch
	Env            []map[string]any
	DisplayName    string
	AssignPublicIP *bool
	Ports          []map[string]any
	Storages       []map[string]any
}

// Launch starts a Sandbox from this Agent. IdcName defaults to the Agent's
// Idc field; launch cannot change region.
//
// Launch returns immediately; call Refresh or WaitUntilRunning on the
// returned Sandbox for current state.
func (a *Agent) Launch(ctx context.Context, params LaunchParams) (*Sandbox, error) {
	idcName := params.IdcName
	if idcName == "" {
		idcName = a.Idc()
	}
	return a.client.Sandboxes.launch(ctx, a.Slug(), params, idcName, a.ID())
}

// Sandboxes lists the Sandboxes launched from this Agent.
func (a *Agent) Sandboxes(ctx context.Context, page, pageSize int) (*Page[*Sandbox], error) {
	if page == 0 {
		page = 1
	}
	if pageSize == 0 {
		pageSize = 20
	}
	var payload struct {
		Items    []map[string]any `json:"items"`
		Total    int              `json:"total"`
		Page     int              `json:"page"`
		PageSize int              `json:"page_size"`
	}
	err := a.client.request(ctx, http.MethodGet, fmt.Sprintf("/deployments/%s/tasks", a.Slug()),
		map[string]any{"page": page, "page_size": pageSize}, nil, nil, &payload)
	if err != nil {
		return nil, err
	}
	items := make([]*Sandbox, 0, len(payload.Items))
	for _, item := range payload.Items {
		items = append(items, &Sandbox{client: a.client, Data: item})
	}
	return &Page[*Sandbox]{
		Items:    items,
		Total:    payload.Total,
		Page:     orDefault(payload.Page, page),
		PageSize: orDefault(payload.PageSize, pageSize),
	}, nil
}

// AgentCollection provides operations on Agents (client.Agents).
type AgentCollection struct {
	client *Client
}

// AgentCreateParams configures AgentCollection.Create.
type AgentCreateParams struct {
	Title                 string
	ImageURL              string
	Idc                   string // required when Runtime == "sandbox"
	Env                   []map[string]any
	Ports                 []map[string]any
	DeploymentType        string // defaults to "gmi-ce" when Runtime == "sandbox"
	InstanceType          string // required when Runtime == "sandbox"
	AssignPublicIP        *bool
	ImageCredential       map[string]any // {"username": "...", "secret": "..."}
	ShortDesc             string
	ShareStorageMountPath string
	CloneSourceID         string
	Runtime               string // "container" or "sandbox"
	StartCmd              string
}

// Create registers a new Agent. Create does not start a container.
func (a *AgentCollection) Create(ctx context.Context, params AgentCreateParams) (*Agent, error) {
	deploymentType := params.DeploymentType
	if deploymentType == "" && params.Runtime == "sandbox" {
		deploymentType = "gmi-ce"
	}

	body := compact(map[string]any{
		"title":                    params.Title,
		"image_url":                params.ImageURL,
		"idc":                      params.Idc,
		"env":                      envOrNil(params.Env),
		"ports":                    portsOrNil(params.Ports),
		"deployment_type":          deploymentType,
		"instance_type":            params.InstanceType,
		"assign_public_ip":         params.AssignPublicIP,
		"image_credential":         mapOrNil(params.ImageCredential),
		"short_desc":               params.ShortDesc,
		"share_storage_mount_path": params.ShareStorageMountPath,
		"clone_source_id":          params.CloneSourceID,
		"runtime":                  params.Runtime,
		"start_cmd":                params.StartCmd,
	})

	var payload map[string]any
	if err := a.client.request(ctx, http.MethodPost, "/deployments", nil, body, nil, &payload); err != nil {
		return nil, err
	}
	return &Agent{client: a.client, Data: payload}, nil
}

// List returns a page of Agents. Pagination is 1-indexed.
func (a *AgentCollection) List(ctx context.Context, page, pageSize int) (*Page[*Agent], error) {
	if page == 0 {
		page = 1
	}
	if pageSize == 0 {
		pageSize = 20
	}
	var payload struct {
		Items    []map[string]any `json:"items"`
		Total    int              `json:"total"`
		Page     int              `json:"page"`
		PageSize int              `json:"page_size"`
	}
	err := a.client.request(ctx, http.MethodGet, "/deployments",
		map[string]any{"page": page, "page_size": pageSize}, nil, nil, &payload)
	if err != nil {
		return nil, err
	}
	items := make([]*Agent, 0, len(payload.Items))
	for _, item := range payload.Items {
		items = append(items, &Agent{client: a.client, Data: item})
	}
	return &Page[*Agent]{
		Items:    items,
		Total:    payload.Total,
		Page:     orDefault(payload.Page, page),
		PageSize: orDefault(payload.PageSize, pageSize),
	}, nil
}

// Get retrieves an Agent by slug.
func (a *AgentCollection) Get(ctx context.Context, slug string) (*Agent, error) {
	var payload map[string]any
	if err := a.client.request(ctx, http.MethodGet, fmt.Sprintf("/deployments/%s", slug), nil, nil, nil, &payload); err != nil {
		return nil, err
	}
	return &Agent{client: a.client, Data: payload}, nil
}

// Update applies a partial update to an Agent by slug. Only send fields the
// API accepts; locked fields return HTTP 422.
func (a *AgentCollection) Update(ctx context.Context, slug string, fields AgentUpdateFields) (*Agent, error) {
	var payload map[string]any
	err := a.client.request(ctx, http.MethodPatch, fmt.Sprintf("/deployments/%s", slug), nil, compact(fields), nil, &payload)
	if err != nil {
		return nil, err
	}
	return &Agent{client: a.client, Data: payload}, nil
}

// Delete deletes an Agent by slug. It does not stop or delete remaining
// sandboxes of that Agent.
func (a *AgentCollection) Delete(ctx context.Context, slug string) error {
	return a.client.request(ctx, http.MethodDelete, fmt.Sprintf("/deployments/%s", slug), nil, nil, nil, nil)
}

func stringField(data map[string]any, key string) string {
	v, _ := data[key].(string)
	return v
}

func intField(data map[string]any, key string) int {
	switch v := data[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	}
	return 0
}

func sliceField(data map[string]any, key string) []any {
	v, _ := data[key].([]any)
	return v
}

func envOrNil(env []map[string]any) any {
	if env == nil {
		return nil
	}
	return env
}

func portsOrNil(ports []map[string]any) any {
	if ports == nil {
		return nil
	}
	return ports
}

func mapOrNil(m map[string]any) any {
	if m == nil {
		return nil
	}
	return m
}

func orDefault(value, fallback int) int {
	if value == 0 {
		return fallback
	}
	return value
}

// Page is a paginated API result.
type Page[T any] struct {
	Items    []T
	Total    int
	Page     int
	PageSize int
}
