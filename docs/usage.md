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
with no `WithAPIKey` option. The key is whitespace-trimmed; a key containing
control characters (anything below 0x20 except tab, or DEL)
is rejected by `NewClient`.

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
	idcID := "us-central-iowa16"
	products, err := client.Products.List(ctx, agentbox.ProductListParams{IdcName: idcID, Runtime: "sandbox"})
	if err != nil {
		panic(err)
	}
	_ = products
	// Choose an instance_type returned above.
	instanceType := "gmi.sandbox.small"

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

- `agent.GeneratedAPIKey()` is returned only by create, and only when create generates a MaaS key: an Agent without `GMI_MODELS` gets none (empty) on both the sandbox and container runtimes. `client.Agents.Get` and `Update` results do not carry it, so store it now. `agent.Refresh` and `agent.Update` keep the in-memory value.
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

A new sandbox starts as `pending`. `WaitUntilRunning` polls until `running`. It returns:

- `*agentbox.SandboxFailed` if status is `error`, `deleted`, `stopped`, or `stopping`
- `*agentbox.SandboxWaitTimeout` if the timeout elapses first

`EndpointURL()` is empty until the sandbox is running. On the sandbox runtime it
holds the data-plane URL once the sandbox is `running`.

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

`Pause` pauses a running sandbox. Disk files are kept (except `/tmp`, which
is cleared on resume); memory and running processes are not. A paused sandbox
stops consuming run time. The call returns when the pause is accepted;
`Status()` is usually `pausing`. Poll with `Refresh` until it is `paused`.
`PausedExpireAt()` is the deadline to resume before the sandbox and its disk
are reclaimed.

```go
err = sandbox.Pause(ctx)
for sandbox.Status() != "paused" {
	time.Sleep(2 * time.Second)
	if err := sandbox.Refresh(ctx); err != nil {
		return err
	}
}
fmt.Println(sandbox.PausedExpireAt())
```

`Resume` wakes a paused sandbox. Omit `TimeoutSeconds` to keep the run time
that was left at pause. Set it (>= 60) to reset that run time and pin the
expiry. The call returns when the resume is accepted; `Status()` is usually
`resuming`. Poll with `WaitUntilRunning`. Resume is a cold start: disk files
come back except `/tmp`, processes are not restored, and the start command is
not re-run.

```go
err = sandbox.Resume(ctx, agentbox.ResumeParams{})
err = sandbox.WaitUntilRunning(ctx, agentbox.WaitUntilRunningParams{})

timeoutSeconds := 3600
err = sandbox.Resume(ctx, agentbox.ResumeParams{TimeoutSeconds: &timeoutSeconds})
```

## Snapshots

A snapshot saves a running sandbox so new sandboxes of the same Agent can
start from it. Creation is asynchronous: the snapshot starts `creating`; wait
for `ready`. Launching from a snapshot inherits its sizing and IDC, so
`InstanceType` is ignored.

```go
snapshot, err := sandbox.CreateSnapshot(ctx, agentbox.CreateSnapshotParams{Name: "base"})
err = snapshot.WaitUntilReady(ctx, agentbox.WaitUntilReadyParams{Timeout: 10 * time.Minute})

clone, err := agent.Launch(ctx, agentbox.LaunchParams{SnapshotID: snapshot.ID()})
err = clone.WaitUntilRunning(ctx, agentbox.WaitUntilRunningParams{})

snapshots, err := agent.ListSnapshots(ctx) // newest first
err = snapshot.Rename(ctx, "base-v2")      // only when ready or error
err = snapshot.Delete(ctx)                 // async; status becomes deleting
```

`WaitUntilReady` returns `*agentbox.SnapshotFailed` if the status becomes
`error`, `deleting` or `delete_failed`, and `*agentbox.SnapshotWaitTimeout`
if the timeout elapses first. A snapshot from
`client.Sandboxes.CreateSnapshot` does not know its Agent; poll it with
`client.Agents.ListSnapshots` instead.

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
with `sandbox.GetExecution(ctx, execution.ID())`. `WaitTimeoutSeconds` accepts
values from 1 through 25 and is ignored when `Wait` is false. File paths (upload
and download) are percent-decoded, then must be absolute and cannot contain `.`
or `..` segments.

## Shell

```go
conn, err := sandbox.Shell(ctx)
if err != nil {
	return err
}
defer conn.Close() // close the connection; do not run `exit`

// First message is {"type":"status","message":"connected"}.
msg, err := agentbox.RecvShellMessage(conn)

err = agentbox.ResizeShell(conn, 120, 32) // terminal size, optional
err = agentbox.SendShellInput(conn, "cd /tmp && pwd\n") // end a command with a newline
for {
	msg, err = agentbox.RecvShellMessage(conn)
	if err != nil {
		return err
	}
	if msg.Type == "stdout" {
		fmt.Print(msg.Data)
		break
	}
}
```

See [examples/shell](../examples/shell) for a complete program.

The shell is a WebSocket to `/tasks/{id}/shell`; redirects are not followed. A
handshake HTTP error (status 300 or above) is an `*APIError` (401 is
`IsAuthenticationError`); other connect failures, including a bad handshake
answered with 101 or 2xx, are a `*TransportError`.

### Protocol

Messages are TEXT frames carrying JSON. Send them with `SendShellInput` and
`ResizeShell`; raw text (`conn.Send`), binary frames and unknown types are
silently ignored by the service. `RecvShellMessage` decodes what the service
sends (`DecodeShellMessage` does the same for a frame you already hold):

| `Type` | Fields | Meaning |
| --- | --- | --- |
| `status` | `Message` | `connected` (first message) or `closed` (the shell ended) |
| `stdout` | `Data` | Raw terminal output |
| `error` | `Message` | A problem, for example a second concurrent attach |

Other types pass through unchanged. A text frame that is not JSON yields
`Type == ""` with `Data` set to the raw text. `Recv` returns the frame
undecoded.

### Session behaviour

- The shell runs as root with bash and `HOME=/root`.
- Output is raw terminal output: it contains the echo of your input, `\r\n`
  line endings and control characters, and stderr is merged into stdout. Use
  `Execute` when you need clean output and exit codes.
- Disconnecting and reconnecting resumes the same session; the working
  directory and exported variables are kept.
- At most one connection is active. A second concurrent attach completes the
  handshake, receives `connected` and then an `error` message, and gets no
  output; the first connection is unaffected.
- Running `exit` makes the service send status `closed` and close the socket.
  Later attaches to that sandbox are closed immediately (`connected`, then
  `closed`). Close the connection instead of running `exit`.
- Latency is not guaranteed; do not depend on timing.
- A connection supports one writer goroutine and one reader goroutine at a
  time; do not call `SendShellInput`, `ResizeShell` or `Send` concurrently.

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
agent.Delete(ctx)   // returns 409 while the Agent still has sandboxes; delete them first
```

`agent.Delete` returns HTTP 409 (`agentbox.IsConflictError`) while the Agent
still has sandboxes, and can keep returning it for a few seconds right after
the last one is deleted. Retry on `IsConflictError`, as
`examples/internal/exampleutil.DeleteAgent` does.

Always delete sandboxes you launched in tests. They bill the Console account.

## Log stream

```go
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
if err := stream.Err(); err != nil {
	log.Fatal(err) // *agentbox.TransportError
}
```

The SDK does not check capabilities; the service decides, and an unsupported
runtime returns an error. `Event` is `message` when unnamed, `heartbeat` for
`:` comment lines, and `status` is sent by the service. `Data` is JSON-decoded
when possible, else the raw string. Close the stream when done.

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

batch, err := sandbox.Metrics(ctx, agentbox.MetricsParams{Start: start, End: end, Kinds: []string{"cpu", "memory"}})
switch {
case agentbox.IsNotSupportedError(err):
	fmt.Println("metrics are not supported by this runtime")
case err != nil:
	panic(err)
default:
	for _, series := range batch.Results {
		fmt.Println(series.Kind, series.EmptyReason, len(series.Points))
	}
}

series, err := sandbox.MetricsTimeseries(ctx, agentbox.MetricsTimeseriesParams{Kind: "cpu", Start: start, End: end})
if err == nil {
	fmt.Println(series.EmptyReason, series.Points)
}
```

`Kinds` omitted means the API default (all 8 charts): `gpu_util`, `gpu_mem`,
`cpu`, `memory`, `disk_read`, `disk_write`, `net_rx`, `net_tx`. `Start` /
`End` are unix seconds. The SDK does not check capabilities; on the sandbox
runtime the service returns HTTP 501 `not_supported_by_runtime`
(`IsNotSupportedError`).

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
is `ErrorKindNotSupported` (template build APIs on a container Agent).
`413`, `415`, `416` and `499` have their own kinds, and `507` is a server
error with `IsInsufficientStorageError`. Connection, URL, redirect and
stream-read failures are `*TransportError` (context cancellation and an
oversize stream line are returned as is). A 3xx response that the HTTP client
did not follow is an `*APIError` (its `Location` is not exposed), also with a
custom `WithTransport`. The SDK never retries. See
[Reference](reference.md).
