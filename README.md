# GMI AgentBox SDK for Go

Thin Go client for AgentBox.

Public types are **Agent** (registered template) and **Sandbox** (launched
container). Registering an Agent does not start anything.

- [Usage](docs/usage.md) — install, auth, register, launch, wait, cleanup
- [Reference](docs/reference.md) — client, methods, models, errors

## Install

```bash
go get github.com/GMISWE/agentbox-sdk-go
```

Requires Go 1.25+.

## Auth

```bash
export GMI_AGENTBOX_API_KEY="your-api-key"
```

`GMI_AGENTBOX_API_KEY` is required (or pass `agentbox.WithAPIKey(...)`).
`GMI_AGENTBOX_BASE_URL` is optional and defaults to production
`https://console.gmicloud.ai`.

## Quickstart

```go
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/GMISWE/agentbox-sdk-go/agentbox"
)

func waitForTemplate(ctx context.Context, agent *agentbox.Agent) error {
	deadline := time.Now().Add(10 * time.Minute)
	for {
		if err := agent.Refresh(ctx); err != nil {
			return err
		}
		switch agent.TemplateBuildStatus() {
		case "", "ready":
			return nil
		case "error":
			return fmt.Errorf("template build failed: %s", agent.TemplateBuildError())
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("template build timed out")
		}
		time.Sleep(3 * time.Second)
	}
}

func main() {
	ctx := context.Background()

	client, err := agentbox.NewClient()
	if err != nil {
		log.Fatal(err)
	}

	// Discover available IDCs and SKUs, then pass explicit values.
	// The SDK does not pick IDC or instance_type for you.
	idcID := "us-central-iowa2"
	instanceType := "gmi.sandbox.x-small"

	agent, err := client.Agents.Create(ctx, agentbox.AgentCreateParams{
		Title:        "agentbox-demo",
		ImageURL:     "docker.io/library/alpine:3.20",
		Idc:          idcID,
		InstanceType: instanceType,
		Runtime:      "sandbox",
	})
	if err != nil {
		log.Fatal(err)
	}
	// agent.GeneratedAPIKey() is plaintext only on this response
	defer agent.Delete(ctx)

	if err := waitForTemplate(ctx, agent); err != nil {
		log.Print(err)
		return
	}

	sandbox, err := agent.Launch(ctx, agentbox.LaunchParams{InstanceType: instanceType})
	if err != nil {
		log.Print(err)
		return
	}
	defer sandbox.Delete(ctx)
	if err := sandbox.WaitUntilRunning(ctx, agentbox.WaitUntilRunningParams{}); err != nil {
		log.Print(err)
		return
	}
	fmt.Println(sandbox.EndpointURL())

	execution, err := sandbox.Execute(ctx, "echo hello", agentbox.ExecuteParams{})
	if err != nil {
		log.Print(err)
		return
	}
	fmt.Println(execution.Status(), execution.Data["stdout"])
}
```

`GET /products?idc_name=` takes an **idcId** from `GET /idcs`, not a region
label. Launch cannot change the Agent's IDC.

`Sandboxes.List` omits `stopped` and `deleted` unless you pass `Status`.

Sandbox runtimes also support command execution, file upload/download, and an
interactive WebSocket shell. See the reference for `Sandbox.Execute`,
`Sandbox.UploadFile`, `Sandbox.DownloadFile`, and `Sandbox.Shell`.
