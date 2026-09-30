# Usage

GMI AgentBox SDK for Go is a thin client for AgentBox.

## Install

```bash
go get github.com/GMISWE/agentbox-sdk-go
```

Requires Go 1.25+.

## Authenticate

To create an AgentBox API key, open
[API keys](https://console.gmicloud.ai/user-setting/ce/access/api-keys), then
click **Create API key**. Store the key securely; it
authenticates requests for your organization.

```bash
export GMI_AGENTBOX_API_KEY="your-api-key"
```

`GMI_AGENTBOX_API_KEY` is required when constructing `agentbox.NewClient()`
with no `WithAPIKey` option.

Or pass it explicitly:

```go
client, err := agentbox.NewClient(agentbox.WithAPIKey("your-api-key"))
```

The default API root is production (used when `GMI_AGENTBOX_BASE_URL` is unset):

`https://api.gmi-serving.com/v1/agents`

Resource paths such as `/deployments` and `/tasks` are joined onto that root.
Override the full root with `GMI_AGENTBOX_BASE_URL` or `agentbox.WithBaseURL(...)`.

## Quickstart

```go
package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/GMISWE/agentbox-sdk-go/agentbox"
)

func waitForTemplate(ctx context.Context, agent *agentbox.Agent, timeout, pollInterval time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if err := agent.Refresh(ctx); err != nil {
			return err
		}
		status := agent.TemplateBuildStatus()
		if status == "ready" || status == "" {
			return nil
		}
		if status == "error" {
			msg := agent.TemplateBuildError()
			if msg == "" {
				msg = "Sandbox image build failed"
			}
			return errors.New(msg)
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("Sandbox image is still %q after %s", status, timeout)
		}
		fmt.Println("...")
		time.Sleep(pollInterval)
	}
}

func main() {
	ctx := context.Background()
	client, err := agentbox.NewClient()
	if err != nil {
		panic(err)
	}

	// Available values vary by organization. Use the catalogue to select an
	// IDC and a SKU for the Sandbox runtime.
	idcs, err := client.Idcs.List(ctx, "sandbox")
	if err != nil {
		panic(err)
	}
	_ = idcs
	// Choose an idc_id returned above. This is the production example value.
	idcID := "us-central-iowa2"
	products, err := client.Products.List(ctx, agentbox.ProductListParams{IdcName: idcID, Runtime: "sandbox"})
	if err != nil {
		panic(err)
	}
	_ = products
	// Choose an instance_type returned above.
	instanceType := "gmi.sandbox.x-small"

	var agent *agentbox.Agent
	var sandbox *agentbox.Sandbox
	defer func() {
		if sandbox != nil {
			sandbox.Delete(ctx)
		}
		if agent != nil {
			agent.Delete(ctx)
		}
	}()

	agent, err = client.Agents.Create(ctx, agentbox.AgentCreateParams{
		Title:        "agentbox-demo", // choose a unique name
		ImageURL:     "docker.io/library/alpine:3.20",
		Idc:          idcID,
		InstanceType: instanceType,
		Runtime:      "sandbox",
	})
	if err != nil {
		panic(err)
	}
	fmt.Println("Building Sandbox image; a new image can take several minutes.")
	if err := waitForTemplate(ctx, agent, 600*time.Second, 3*time.Second); err != nil {
		panic(err)
	}

	sandbox, err = agent.Launch(ctx, agentbox.LaunchParams{InstanceType: instanceType})
	if err != nil {
		panic(err)
	}
	if err := sandbox.WaitUntilRunning(ctx, agentbox.WaitUntilRunningParams{}); err != nil {
		var failed *agentbox.SandboxFailed
		if errors.As(err, &failed) {
			fmt.Println("failed", failed.Status, failed.Message)
			return
		}
		var timedOut *agentbox.SandboxWaitTimeout
		if errors.As(err, &timedOut) {
			fmt.Println("still not running", sandbox.Status(), sandbox.LastError())
			return
		}
		panic(err)
	}
	fmt.Println(sandbox.Status(), sandbox.EndpointURL())

	execution, err := sandbox.Execute(ctx, "echo hello", agentbox.ExecuteParams{})
	if err != nil {
		panic(err)
	}
	fmt.Println(execution.Status(), execution.ExitCode(), execution.Data["stdout"])
}
```

Use `client.Idcs.List(ctx, "sandbox")` and
`client.Products.List(ctx, agentbox.ProductListParams{IdcName: ..., Runtime: "sandbox"})`
to discover available Sandbox data centers and SKUs.

## Register an Agent

```go
agent, err := client.Agents.Create(ctx, agentbox.AgentCreateParams{
	Title:        "agentbox-demo",
	ImageURL:     "docker.io/library/alpine:3.20",
	Idc:          idcID,
	InstanceType: instanceType,
	Runtime:      "sandbox",
	Env:          []map[string]any{{"name": "GMI_MODELS", "value": "llama", "secret": false}},
})
fmt.Println(agent.Slug(), agent.Launchable())
```

- `agent.GeneratedAPIKey()` is plaintext only on create (and rare patch retries).
- `agent.Launchable()` is false when the upstream template is missing.

Later:

```go
agent, err := client.Agents.Get(ctx, "agentbox-demo")
page, err := client.Agents.List(ctx, 1, 20)
err = agent.Update(ctx, agentbox.AgentUpdateFields{"title": "renamed"})
err = agent.Delete(ctx)
```

## Launch a Sandbox

```go
sandbox, err := agent.Launch(ctx, agentbox.LaunchParams{InstanceType: instanceType})
// or: client.Sandboxes.Launch(ctx, agent.Slug(), agentbox.LaunchParams{InstanceType: ..., IdcName: agent.Idc()})

err = sandbox.WaitUntilRunning(ctx, agentbox.WaitUntilRunningParams{
	Timeout:      300 * time.Second,
	PollInterval: 2 * time.Second,
})
err = sandbox.Refresh(ctx)
fmt.Println(sandbox.Status(), sandbox.LastError(), sandbox.EndpointURL())
```

`WaitUntilRunning` polls until `running`. It returns:

- `*agentbox.SandboxFailed` if status is `error`, `deleted`, `stopped`, or `stopping`
- `*agentbox.SandboxWaitTimeout` if the timeout elapses first

`EndpointURL()` is empty until the sandbox is running, and can still be empty
after `running`. Do not assume an HTTP URL is always present.

## Metadata and expiry

Both are sandbox-runtime only. `Metadata` is your own labels.
`TimeoutSeconds` is the lifetime in seconds from now. Leave it unset to keep
the platform TTL and auto-renew. A positive value pins the expiry. Read the
effective timestamp from `sandbox.ExpiresAt()` after `Refresh` or
`SetTimeout`.

```go
timeoutSeconds := 3600
sandbox, err := agent.Launch(ctx, agentbox.LaunchParams{
	InstanceType:   instanceType,
	Metadata:       map[string]string{"session": "abc"},
	TimeoutSeconds: &timeoutSeconds,
})
err = sandbox.Refresh(ctx)
fmt.Println(sandbox.Metadata(), sandbox.ExpiresAt(), sandbox.ExpiryPinned())

err = sandbox.SetTimeout(ctx, 6*3600)
page, err := client.Sandboxes.List(ctx, agentbox.SandboxListParams{
	Metadata: map[string]string{"session": "abc"},
})
```

`SetTimeout` works while the sandbox is running or paused. It can shorten or
extend the remaining lifetime, and it pins the sandbox so later reads do not
auto-renew it.

## Run commands and transfer files

```go
// Finished command
result, err := sandbox.Execute(ctx, "echo hello", agentbox.ExecuteParams{})
fmt.Println(result.Status(), result.ExitCode(), result.Data["stdout"])

// Start a long-running command, then cancel it.
wait := false
execution, err := sandbox.Execute(ctx, "sleep 120", agentbox.ExecuteParams{Wait: &wait})
if execution.Accepted {
	sandbox.CancelExecution(ctx, execution.ID())
}

// File round trip
sandbox.UploadFile(ctx, "/home/user/input.txt", "input.txt", bytes.NewReader([]byte("hello\n")))
download, err := sandbox.DownloadFile(ctx, "/home/user/input.txt")
fmt.Println(download.Filename, download.Content)
```

`Wait: &wait` (false) returns an accepted execution; retrieve its latest state
with `sandbox.GetExecution(ctx, execution.ID())`. File paths must be absolute
and cannot contain `.` or `..` segments.

## List and filter sandboxes

```go
page, err := client.Sandboxes.List(ctx, agentbox.SandboxListParams{}) // omits stopped and deleted
page, err = client.Sandboxes.List(ctx, agentbox.SandboxListParams{Status: []string{"running", "creating", "error"}})
page, err = client.Sandboxes.List(ctx, agentbox.SandboxListParams{AgentID: agent.ID(), Status: []string{"running"}})
page, err = client.Sandboxes.List(ctx, agentbox.SandboxListParams{Metadata: map[string]string{"session": "abc"}})
page, err = agent.ListSandboxes(ctx, agentbox.AgentSandboxListParams{Metadata: map[string]string{"session": "abc"}})
```

The default Sandbox list **excludes** `stopped` and `deleted`.
Pass `Status` when you need those.

## Delete

```go
sandbox.Delete(ctx) // one-way; the sandbox cannot be restarted
agent.Delete(ctx)   // does not stop or delete remaining sandboxes
```

Always delete sandboxes you launched in tests. They bill the Console account.

## Logs

The SDK supports log snapshots and streaming when the Sandbox provides the
corresponding capability. Check `Capabilities()` for `logs` and `logs_stream`
before calling them:

```go
if caps := sandbox.Capabilities(); caps["logs"] == true {
	logs, err := sandbox.Logs(ctx)
	fmt.Println(logs)
}

if caps := sandbox.Capabilities(); caps["logs_stream"] == true {
	stream, err := sandbox.Stream(ctx)
	if err != nil {
		log.Fatal(err)
	}
	defer stream.Close()
	for {
		event, ok := stream.Next()
		if !ok {
			break
		}
		fmt.Println(event.Event, event.Data)
		if event.Event == "status" {
			break
		}
	}
}
```

When unavailable, these calls return an unsupported-runtime error. A backend
can enable the capability without requiring an SDK update.

## Template build logs

Sandbox Agents expose paged template build logs. Container Agents return
`501` (`IsNotSupportedError`). GMI does not persist these logs.

```go
func followBuildLogs(ctx context.Context, agent *agentbox.Agent) error {
	builds, err := agent.ListBuilds(ctx)
	if err != nil {
		return err
	}
	if len(builds) == 0 {
		return fmt.Errorf("no template builds")
	}
	buildID := builds[0].ID
	offset := 0
	for {
		page, err := agent.BuildLogs(ctx, buildID, agentbox.BuildLogParams{Offset: offset})
		if agentbox.IsGoneError(err) {
			fmt.Println("logs no longer available")
			return nil
		}
		if err != nil {
			return err
		}
		for _, entry := range page.Entries {
			fmt.Println(entry.Level, entry.Message)
		}
		offset = page.NextOffset
		if page.HasMore {
			continue
		}
		build, err := agent.GetBuild(ctx, buildID)
		if err != nil {
			return err
		}
		if build.Status == "ready" || build.Status == "error" {
			final, err := agent.BuildLogs(ctx, buildID, agentbox.BuildLogParams{Offset: offset})
			if agentbox.IsGoneError(err) {
				fmt.Println("logs no longer available")
				return nil
			}
			if err != nil {
				return err
			}
			for _, entry := range final.Entries {
				fmt.Println(entry.Level, entry.Message)
			}
			if build.Status == "error" && build.Failure != nil {
				return fmt.Errorf("%s", build.Failure.Message)
			}
			return nil
		}
		time.Sleep(2 * time.Second)
	}
}
```

`HasMore` false only means nothing more is available right now. Keep polling
from `NextOffset` while the build is `waiting` or `building`, then read one
last page after it reaches `ready` or `error`. `410` means the logs are gone,
including for a successful build.

## Eligibility and metrics

```go
entitlement, err := client.Eligibility(ctx)
fmt.Println(entitlement.Eligible, entitlement.DataCenters)

if caps := sandbox.Capabilities(); caps["metrics"] == true {
	batch, err := sandbox.Metrics(ctx, agentbox.MetricsParams{Start: start, End: end, Kinds: []string{"cpu", "memory"}})
	series, err := sandbox.MetricsTimeseries(ctx, agentbox.MetricsTimeseriesParams{Kind: "cpu", Start: start, End: end})
	fmt.Println(series.EmptyReason, series.Points)
}
```

`Kinds` omitted means the API default (all 8 charts): `gpu_util`, `gpu_mem`,
`cpu`, `memory`, `disk_read`, `disk_write`, `net_rx`, `net_tx`. `Start` /
`End` are unix seconds. Metrics are callable only when the Sandbox provides
the `metrics` capability; otherwise the service reports that the runtime does
not support them.

## Concepts

| Name | Meaning |
|---|---|
| `Idc` / `IdcName` | Data center identifier, not a region label. |
| `InstanceType` | Billing SKU, not the product object name. |

## Errors

```go
agent, err := client.Agents.Get(ctx, "missing")
var apiErr *agentbox.APIError
if errors.As(err, &apiErr) {
	fmt.Println(apiErr.StatusCode, apiErr.Message)
}
if agentbox.IsNotFoundError(err) {
	// handle 404
}
```

`401` is `ErrorKindAuthentication` (for example, when the API key is
invalid). `422` is `ErrorKindUnprocessable` (locked field / template
mismatch). `410` is `ErrorKindGone` (template build logs reclaimed). `501`
is `ErrorKindNotSupported` (template build APIs on a container Agent) and is
not retried as a server error. See [Reference](https://docs.gmicloud.ai/api-reference/agentbox-sdk/reference).
