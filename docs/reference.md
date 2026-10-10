# Reference

Module: `github.com/GMISWE/agentbox-sdk-go`
Package: `agentbox`

See [Usage](usage.md) for workflows.

## Configuration

### `agentbox.NewClient`

```go
func NewClient(opts ...Option) (*Client, error)
```

| Option | Default | Notes |
|---|---|---|
| `WithAPIKey(key)` | `GMI_AGENTBOX_API_KEY` | Required API key. `NewClient` errors if unset |
| `WithBaseURL(url)` | `GMI_AGENTBOX_BASE_URL` or `DefaultBaseURL` | Optional API root, including the path prefix. Production root if unset |
| `WithTimeout(d)` | `30 * time.Second` | Per HTTP call |
| `WithHTTPClient(c)` | internal default | Overrides the `*http.Client` used by the default transport |
| `WithTransport(t)` | internal default | Overrides the request/response transport (for testing) |
| `WithStreamTransport(t)` | internal default | Overrides the streaming transport (for testing) |
| `WithSleep(f)` | `time.Sleep` | Overrides the sleep function used by `WaitUntilRunning` polling |

`DefaultBaseURL` is `https://api.gmi-serving.com/v1/agents`.
`DefaultWaitTimeout` is `300 * time.Second` (`Sandbox.WaitUntilRunning`).

Collections on the client: `Agents`, `Sandboxes`, `Idcs`, `Products`.
Also `client.Eligibility(ctx)`.

#### `client.Health() map[string]any`

Returns local client configuration.

```go
map[string]any{"status": "ok", "baseUrl": "...", "authenticated": true}
```

#### `client.Eligibility(ctx) (*Eligibility, error)`

Returns the organization's available runtimes and data centers.

### Environment variables

| Variable | Used by |
|---|---|
| `GMI_AGENTBOX_API_KEY` | Required API key |
| `GMI_AGENTBOX_BASE_URL` | Optional API root; defaults to `DefaultBaseURL` |

Errors expose `Message`, `Code`, and `Details` when supplied by the service.

## Agents

### `client.Agents`

#### `Create(ctx, AgentCreateParams) (*Agent, error)`

| Field | Required | Details |
|---|---|---|
| `Title` | yes | Agent name |
| `ImageURL` | no | Container image |
| `Idc` | Sandbox: yes; otherwise no | Data center identifier; required with `Runtime: "sandbox"` |
| `Runtime` | no | `"container"` or `"sandbox"`; defaults to the service default |
| `StartCmd` | no | Startup command for a Sandbox |
| `Env` | no | Environment-variable definitions |
| `Ports` | no | Port definitions |
| `InstanceType` | Sandbox: yes; otherwise no | Required with `Runtime: "sandbox"` |
| `AssignPublicIP` | no | Whether to request a public IP |
| `ImageCredential` | no | Private-registry credentials: `{"username": "...", "secret": "..."}` |
| `ShortDesc` | no | Short description |
| `ShareStorageMountPath` | no | Shared-storage mount path |
| `CloneSourceID` | no | Source Agent ID to clone |

Create does not start a container.

#### `List(ctx, page, pageSize int) (*Page[*Agent], error)`

Pagination is 1-indexed; `0` for either argument defaults to `page=1`,
`pageSize=20`. API default page size is 20, max 100.

#### `Get(ctx, slug string) (*Agent, error)`

#### `Update(ctx, slug string, AgentUpdateFields) (*Agent, error)`

PATCH passthrough (`AgentUpdateFields` is `map[string]any`). Only send fields
the API accepts; locked fields return `422`.

#### `Delete(ctx, slug string) error`

Does not stop or delete running sandboxes of that Agent.

### `Agent`

Represents an Agent.

| Method | Type | Notes |
|---|---|---|
| `ID()` | `string` | Agent ID |
| `Slug()` | `string` | Stable Agent identifier |
| `Title()` | `string` | |
| `Idc()` | `string` | Data center identifier |
| `ImageURL()` | `string` | |
| `Runtime()` | `string` | Selected runtime |
| `TemplateBuildStatus()` | `string` | Sandbox image-build status |
| `TemplateBuildError()` | `string` | Sandbox image-build error |
| `Env()` | `[]any` | Copy of `env` |
| `Revision()` | `int` | |
| `GeneratedAPIKey()` | `string` | Plaintext MaaS key on **create** only |
| `Launchable()` | `bool` | Whether the Agent can be launched |

#### `agent.Refresh(ctx) error`

Reloads the Agent in place.

#### `agent.Update(ctx, AgentUpdateFields) error`

Same as `client.Agents.Update(ctx, agent.Slug(), fields)`, applied in place.

#### `agent.Delete(ctx) error`

#### `agent.Launch(ctx, LaunchParams) (*Sandbox, error)`

`LaunchParams.IdcName` defaults to `agent.Idc()` when empty.
Launch cannot change region; `IdcName` must match the Agent's data center.

`Metadata` is sandbox-runtime only: up to 32 keys, key
`^[A-Za-z0-9_.-]{1,63}$`, value at most 256 characters. `source` and
`task_name` are reserved. A non-empty map on a container deployment is
HTTP 422 `sandbox_only_field`. Invalid metadata is HTTP 400
`invalid_metadata`.

`TimeoutSeconds` is the sandbox lifetime in seconds from launch. Leave it
nil, or set `0`, to keep the platform TTL and auto-renew. A positive value
is sent as `timeout_seconds` and pins the expiry. The minimum is 60; a
smaller value is HTTP 400 `invalid_timeout`. A non-zero value on a container
deployment is HTTP 422 `sandbox_only_field`. Launch does not return the
effective expiry — call `Refresh` and read `ExpiresAt`.

Launch returns immediately. Call `Refresh` or `WaitUntilRunning` for
current state.

#### `agent.Sandboxes(ctx, page, pageSize int) (*Page[*Sandbox], error)`

Sandboxes for this Agent.

#### `agent.ListSandboxes(ctx, AgentSandboxListParams) (*Page[*Sandbox], error)`

Same list. `Metadata` is an exact-match filter; keys are ANDed as
`metadata[<key>]=<value>`.

#### `ListBuilds(ctx, slug string) ([]TemplateBuild, error)`

`agent.ListBuilds(ctx)` calls this with `agent.Slug()`.

Returns up to the newest 100 sandbox template builds. There is no page
parameter. Container deployments return `501` (`IsNotSupportedError`).

#### `GetBuild(ctx, slug, buildID string) (TemplateBuild, error)`

`agent.GetBuild(ctx, buildID)` calls this with `agent.Slug()`.

Same object as one `ListBuilds` item. Poll `Status` until `ready` or `error`.

#### `BuildLogs(ctx, slug, buildID string, BuildLogParams) (BuildLogPage, error)`

`agent.BuildLogs(ctx, buildID, params)` calls this with `agent.Slug()`.

Logs are paged, not streamed.

| Field | Query | Default | Notes |
|---|---|---|---|
| `Offset` | `offset` | `0` | Zero-based. Pass the previous page's `NextOffset`. |
| `Limit` | `limit` | `100` | Page size, 1–100. `0` sends `100`. |
| `Level` | `level` | omitted | Optional: `debug`, `info`, `warn`, or `error`. |

`HasMore` false means nothing more is available right now. While `Status` is
`waiting` or `building`, poll again from `NextOffset`. After the build
reaches `ready` or `error`, read one last page from that offset.

`410` (`IsGoneError`, message `build_logs_expired`) means the runtime
reclaimed the logs. Stop reading. That includes successful builds and is not
a build failure. GMI does not persist these logs.

## Sandboxes

### `client.Sandboxes`

#### `Launch(ctx, slug string, LaunchParams) (*Sandbox, error)`

Same body as `agent.Launch`. `LaunchParams.IdcName` is required here.

#### `List(ctx, SandboxListParams) (*Page[*Sandbox], error)`

| Field | Query |
|---|---|
| `AgentID` | Restrict results to one Agent |
| `Status` | `[]string`; joined with commas |
| `Metadata` | Exact match per key, ANDed: `metadata[<key>]=<value>` |
| `Page` / `PageSize` | pagination |

If `Status` is omitted, the API **excludes** `stopped` and `deleted`.

#### `SetTimeout(ctx, sandboxID string, timeoutSeconds int) (*SandboxExpiry, error)`

Resets the lifetime to now + `timeoutSeconds` and pins it, so the sandbox is
no longer auto-renewed. `timeoutSeconds` must be >= 60. Only a running or
paused sandbox accepts it. `SandboxExpiry.ExpiresAt` is the upstream
effective expiry.

| Status | Message | Meaning |
|---|---|---|
| 400 | `invalid_timeout` | Lifetime is under 60 seconds or above the account maximum |
| 409 | `task_not_running` | Sandbox is not running or paused |
| 422 | `sandbox_only_operation` | Task is not a sandbox |

#### `Resume(ctx, sandboxID string, ResumeParams) (*SandboxResume, error)`

Wakes a paused sandbox. `POST /tasks/{id}/resume`, HTTP 202. `TimeoutSeconds`
nil omits the body and keeps the run time left at pause; the expiry pin is
unchanged. A non-nil value must be >= 60. It resets the run time and pins the
expiry, the same as `SetTimeout`.

`SandboxResume.State` is the upstream state at acceptance, usually `resuming`.
Resume is a cold start: disk files return except `/tmp`, processes are not
restored, and the start command is not re-run. Poll `Get` or
`sandbox.WaitUntilRunning` until `running`.

| Status | Message | Meaning |
|---|---|---|
| 400 | `invalid_timeout` | `timeout_seconds` is under 60 or above the account maximum |
| 409 | `task_not_running` | The sandbox does not exist yet |
| 409 | `task_not_paused` | The sandbox is not paused |
| 422 | `sandbox_only_operation` | Task is not a sandbox |

#### `Get(ctx, sandboxID string) (*Sandbox, error)`

#### `Delete(ctx, sandboxID string) error`

One-way. The sandbox cannot be restarted.

#### `Metrics(ctx, sandboxID string, MetricsParams) (*MetricsBatch, error)`

`Start` / `End` are unix seconds. `Kinds` is a `[]string`
(`["cpu", "memory"]`); omit for all 8 charts. `Step` default on the API is
60s (min 5, max 3600).

#### `MetricsTimeseries(ctx, sandboxID string, MetricsTimeseriesParams) (*MetricSeries, error)`

`Kind` is one of `gpu_util`, `gpu_mem`, `cpu`, `memory`, `disk_read`,
`disk_write`, `net_rx`, `net_tx`.

#### `Stream(ctx, sandboxID string) (*Stream, error)`

Pull with `stream.Next() (StreamEvent, bool)`; check `stream.Err()` after
`Next` returns `false`. Comment lines (`: ping`) are `Event="heartbeat"`.
4xx/5xx return the same `*APIError` types as other calls.

Metrics and stream methods require the corresponding Sandbox capability to
be available.

#### `Execute(ctx, sandboxID, command string, ExecuteParams) (*Execution, error)`

Runs a command in a Sandbox. A completed command returns `Accepted=false`; an
accepted asynchronous command returns `Accepted=true`.

#### `GetExecution(ctx, sandboxID, executionID string) (*Execution, error)`

Retrieves the latest execution state.

#### `CancelExecution(ctx, sandboxID, executionID string) (*Execution, error)`

Requests cancellation of an execution.

#### `UploadFile(ctx, sandboxID, sandboxPath, filename string, content io.Reader) ([]map[string]any, error)`

Uploads content to an absolute Sandbox path. `filename` defaults to
`path.Base(sandboxPath)` when empty.

#### `DownloadFile(ctx, sandboxID, sandboxPath string) (*FileDownload, error)`

Downloads a file from an absolute Sandbox path.

### `Sandbox`

Represents a Sandbox.

| Method | Type | Notes |
|---|---|---|
| `ID()` | `string` | Sandbox ID |
| `Status()` | `string` | Current lifecycle status |
| `EndpointURL()` | `string` | Empty until running (and can still be empty after) |
| `AgentID()` | `string` | Owning Agent ID |
| `AgentSlug()` | `string` | Owning Agent slug |
| `DisplayName()` | `string` | |
| `InstanceType()` | `string` | Billing SKU, not the product object name |
| `IdcName()` | `string` | Data center identifier |
| `StatusStale()` | `bool` | |
| `LastError()` | `string` | |
| `Runtime()` | `string` | Runtime name, e.g. `sandbox` |
| `Capabilities()` | `map[string]any` | Server-derived enabled surfaces |
| `ExpiresAt()` | `string` | Sandbox expiry timestamp |
| `ExpiryPinned()` | `bool` | Caller chose the expiry; no auto-renew |
| `Metadata()` | `map[string]string` | Launch labels; nil when unset |

#### `sandbox.SetTimeout(ctx, timeoutSeconds int) error`

Same as `client.Sandboxes.SetTimeout`. Updates `ExpiresAt` and
`ExpiryPinned` from the response.

#### `sandbox.Resume(ctx, ResumeParams) error`

Same as `client.Sandboxes.Resume`. Sets `Status()` from `State` and updates
`ExpiryPinned` from the response.

#### `sandbox.Refresh(ctx) error`

#### `sandbox.Metrics(ctx, MetricsParams) (*MetricsBatch, error)`

#### `sandbox.MetricsTimeseries(ctx, MetricsTimeseriesParams) (*MetricSeries, error)`

#### `sandbox.Stream(ctx) (*Stream, error)`

#### `sandbox.Execute(ctx, command string, ExecuteParams) (*Execution, error)`

Runs a command. `Execution.Accepted` is true when execution continues
asynchronously; use `GetExecution` to poll it, or `CancelExecution` to
request cancellation. `WaitTimeoutSeconds` is ignored when `Wait` is false.

#### `sandbox.GetExecution(ctx, executionID string) (*Execution, error)`

#### `sandbox.CancelExecution(ctx, executionID string) (*Execution, error)`

#### `sandbox.UploadFile(ctx, sandboxPath, filename string, content io.Reader) ([]map[string]any, error)`

#### `sandbox.DownloadFile(ctx, sandboxPath string) (*FileDownload, error)`

Returns `FileDownload{Content, Filename, Header}`. Paths must be absolute and
cannot contain `.` or `..` segments.

#### `sandbox.Delete(ctx) error`

#### `sandbox.WaitUntilRunning(ctx, WaitUntilRunningParams) error`

Polls until `Status() == "running"`. Defaults: `Timeout: DefaultWaitTimeout`,
`PollInterval: 2 * time.Second`.

| Outcome | Result |
|---|---|
| `running` | returns `nil` |
| `error`, `deleted`, `stopped`, `stopping` | returns `*SandboxFailed` |
| timeout | returns `*SandboxWaitTimeout` |

`SandboxFailed.Status` is the sandbox status; `SandboxFailed.Message` is
`LastError()` when set.

## Catalog

### `client.Idcs`

#### `List(ctx, runtime string) ([]*Idc, error)`

### `Idc`

| Method | Description |
|---|---|
| `IdcID()` | Data center identifier |
| `Name()` | Display name |

Use `IdcID()` when selecting an Agent or Sandbox location.

### `client.Products`

#### `List(ctx, ProductListParams) ([]*Product, error)`

`IdcName` is a data center identifier from `Idcs.List`, not a region label.

### `Product`

| Method | Description |
|---|---|
| `InstanceType()` | SKU identifier |
| `Price()` | Price reported by the service |

## Result types

### `TemplateBuild`

| Field | Type | Notes |
|---|---|---|
| `ID` | `string` | Build ID |
| `TemplateID` | `string` | Sandbox template ID |
| `Status` | `string` | `waiting`, `building`, `ready`, or `error` |
| `Trigger` | `string` | For example `initial_build` |
| `SourceType` | `string` | For example `image` |
| `ArtifactState` | `string` | For example `absent` |
| `CreatedAt` | `time.Time` | |
| `StartedAt` | `*time.Time` | Nil until the build starts |
| `FinishedAt` | `*time.Time` | Nil until the build finishes |
| `Failure` | `*TemplateBuildFailure` | Set only when `Status` is `error` |

`TemplateBuildFailure` fields: `Code` (for example `TemplateBuild.OutOfMemory`)
and `Message`. `Message` is safe to show to end users.

### `BuildLogPage`

| Field | Type | Notes |
|---|---|---|
| `Entries` | `[]BuildLogEntry` | Log lines in this page |
| `NextOffset` | `int` | Pass as the next `Offset` |
| `HasMore` | `bool` | False means nothing more right now |

`BuildLogEntry` fields: `Offset`, `Level`, `Message`, `Timestamp` (`*time.Time`,
nil when the runtime omits it).

`BuildLogParams` fields: `Offset`, `Limit`, `Level`. See `BuildLogs`.

### `Page[T]`

```go
type Page[T any] struct {
	Items    []T // []*Agent or []*Sandbox
	Total    int
	Page     int
	PageSize int
}
```

### `Execution`

| Method | Type | Description |
|---|---|---|
| `ID()` | `string` | Execution identifier |
| `Status()` | `string` | Current execution status |
| `ExitCode()` | `*int` | Process exit code when complete, or `nil` |
| `Accepted` | `bool` | True when execution continues asynchronously |
| `Data` | `map[string]any` | Execution details, including command output when provided |

### `FileDownload`

| Field | Type | Description |
|---|---|---|
| `Content` | `[]byte` | Downloaded file content |
| `Filename` | `string` | Download filename |
| `Header` | `http.Header` | Response metadata |

## Errors

All SDK errors are ordinary Go `error` values; use `errors.As` to unwrap
`*APIError`, `*SandboxFailed`, `*SandboxWaitTimeout`, or `*TransportError`.

| Type | When |
|---|---|
| `*TransportError` | The service could not be reached |
| `*APIError` | The server rejected a request; `Kind` distinguishes the variant below |
| `*SandboxFailed` | Wait hit a terminal non-running status |
| `*SandboxWaitTimeout` | Wait exceeded `Timeout` |

`APIError.Kind` values and matching helpers:

| `ErrorKind` | Helper | HTTP status |
|---|---|---|
| `ErrorKindBadRequest` | `IsBadRequestError(err)` | 400 |
| `ErrorKindAuthentication` | `IsAuthenticationError(err)` | 401 |
| `ErrorKindPermissionDenied` | `IsPermissionDeniedError(err)` | 403 |
| `ErrorKindNotFound` | `IsNotFoundError(err)` | 404 |
| `ErrorKindConflict` | `IsConflictError(err)` | 409 |
| `ErrorKindUnprocessable` | `IsUnprocessableError(err)` | 422 |
| `ErrorKindGone` | `IsGoneError(err)` | 410 |
| `ErrorKindRateLimit` | `IsRateLimitError(err)` | 429 |
| `ErrorKindNotSupported` | `IsNotSupportedError(err)` | 501 |
| `ErrorKindServer` | `IsServerError(err)` | 5xx other than 501 |
| `ErrorKindGeneric` | — | other |

`APIError` fields: `StatusCode`, `Message`, `Code`, `Details`.

## Public exports

```go
import "github.com/GMISWE/agentbox-sdk-go/agentbox"
```

```
DefaultBaseURL, DefaultWaitTimeout, Version
Client, Option, WithAPIKey, WithBaseURL, WithTimeout, WithHTTPClient,
    WithTransport, WithStreamTransport, WithSleep
Agent, AgentCollection, AgentCreateParams, AgentUpdateFields, LaunchParams,
    AgentSandboxListParams
TemplateBuild, TemplateBuildFailure, BuildLogEntry, BuildLogPage, BuildLogParams
Sandbox, SandboxCollection, SandboxListParams, SandboxExpiry, ResumeParams,
    SandboxResume, ExecuteParams,
    WaitUntilRunningParams, MetricsParams, MetricsTimeseriesParams
Execution, FileDownload, MetricSeries, MetricsBatch
Idc, IdcCollection, Product, ProductCollection, ProductListParams
Eligibility, Page[T]
StreamEvent, Stream
TransportError, SandboxFailed, SandboxWaitTimeout
APIError, ErrorKind (+ ErrorKind* constants), Is*Error helpers
```
