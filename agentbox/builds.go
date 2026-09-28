package agentbox

import (
	"context"
	"net/http"
	"time"
)

const defaultBuildLogLimit = 100

// TemplateBuildFailure is present only when a template build failed.
// Message is safe to show to end users.
type TemplateBuildFailure struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// TemplateBuild is one sandbox template build.
// Status moves waiting → building → ready or error.
type TemplateBuild struct {
	ID            string                `json:"id"`
	TemplateID    string                `json:"template_id"`
	Status        string                `json:"status"`
	Trigger       string                `json:"trigger"`
	SourceType    string                `json:"source_type"`
	ArtifactState string                `json:"artifact_state"`
	CreatedAt     time.Time             `json:"created_at"`
	StartedAt     *time.Time            `json:"started_at"`
	FinishedAt    *time.Time            `json:"finished_at"`
	Failure       *TemplateBuildFailure `json:"failure"`
}

// BuildLogEntry is one template build log line.
// Timestamp may be nil when the runtime omits it.
type BuildLogEntry struct {
	Offset    int        `json:"offset"`
	Level     string     `json:"level"`
	Message   string     `json:"message"`
	Timestamp *time.Time `json:"timestamp"`
}

// BuildLogPage is one page of template build log entries.
// HasMore false means nothing more is available right now. While the build
// is still waiting or building, new entries can arrive; poll again from
// NextOffset.
type BuildLogPage struct {
	Entries    []BuildLogEntry `json:"entries"`
	NextOffset int             `json:"next_offset"`
	HasMore    bool            `json:"has_more"`
}

// BuildLogParams selects a page of template build log entries.
// Offset is the zero-based entry offset; pass the previous page's NextOffset.
// Limit is the page size, from 1 to 100. Zero uses the API default of 100.
// Level optionally filters to debug, info, warn, or error.
type BuildLogParams struct {
	Offset int
	Limit  int
	Level  string
}

// ListBuilds returns up to the newest 100 template builds for a sandbox
// Agent. Container Agents return HTTP 501 (IsNotSupportedError).
func (a *Agent) ListBuilds(ctx context.Context) ([]TemplateBuild, error) {
	return a.client.Agents.ListBuilds(ctx, a.Slug())
}

// GetBuild returns one template build. The shape matches one ListBuilds item.
func (a *Agent) GetBuild(ctx context.Context, buildID string) (TemplateBuild, error) {
	return a.client.Agents.GetBuild(ctx, a.Slug(), buildID)
}

// BuildLogs reads one page of template build logs. Logs are paged, not
// streamed. HTTP 410 (IsGoneError) means the runtime reclaimed the logs;
// stop reading. That includes successful builds and is not a build failure.
func (a *Agent) BuildLogs(ctx context.Context, buildID string, params BuildLogParams) (BuildLogPage, error) {
	return a.client.Agents.BuildLogs(ctx, a.Slug(), buildID, params)
}

// ListBuilds returns up to the newest 100 template builds for a sandbox
// deployment slug. Container deployments return HTTP 501 (IsNotSupportedError).
func (a *AgentCollection) ListBuilds(ctx context.Context, slug string) ([]TemplateBuild, error) {
	var payload struct {
		Builds []TemplateBuild `json:"builds"`
	}
	err := a.client.request(ctx, http.MethodGet, "/deployments/"+slug+"/builds", nil, nil, nil, &payload)
	if err != nil {
		return nil, err
	}
	if payload.Builds == nil {
		payload.Builds = []TemplateBuild{}
	}
	return payload.Builds, nil
}

// GetBuild returns one template build for a deployment slug.
func (a *AgentCollection) GetBuild(ctx context.Context, slug, buildID string) (TemplateBuild, error) {
	var build TemplateBuild
	err := a.client.request(ctx, http.MethodGet, "/deployments/"+slug+"/builds/"+buildID, nil, nil, nil, &build)
	if err != nil {
		return TemplateBuild{}, err
	}
	return build, nil
}

// BuildLogs reads one page of template build logs for a deployment slug.
// See Agent.BuildLogs.
func (a *AgentCollection) BuildLogs(ctx context.Context, slug, buildID string, params BuildLogParams) (BuildLogPage, error) {
	var page BuildLogPage
	err := a.client.request(ctx, http.MethodGet, "/deployments/"+slug+"/builds/"+buildID+"/logs", buildLogQuery(params), nil, nil, &page)
	if err != nil {
		return BuildLogPage{}, err
	}
	if page.Entries == nil {
		page.Entries = []BuildLogEntry{}
	}
	return page, nil
}

func buildLogQuery(params BuildLogParams) map[string]any {
	limit := params.Limit
	if limit == 0 {
		limit = defaultBuildLogLimit
	}
	query := map[string]any{
		"offset": params.Offset,
		"limit":  limit,
	}
	if params.Level != "" {
		query["level"] = params.Level
	}
	return query
}
