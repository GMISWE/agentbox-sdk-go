// Command quickstart walks the whole sandbox lifecycle: pick a data center and
// SKU, register an Agent, wait for its template, launch a sandbox, run a
// command, move a file in and out, and clean up.
//
// It creates billable resources and deletes them before it exits.
//
//	export GMI_AGENTBOX_API_KEY=...
//	export GMI_AGENTBOX_IDC=...            # optional; discovered when unset
//	export GMI_AGENTBOX_INSTANCE_TYPE=...  # optional; discovered when unset
//	go run ./examples/quickstart
package main

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/GMISWE/agentbox-sdk-go/agentbox"
)

func main() {
	ctx := context.Background()

	client, err := agentbox.NewClient()
	if err != nil {
		log.Fatal(err)
	}

	// 1. Choose where to run. The SDK does not pick an IDC or SKU for you.
	idc, instanceType, err := pickPlacement(ctx, client)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("data center:", idc, "instance type:", instanceType)

	// 2. Register an Agent. This builds a sandbox template from the image but
	// starts nothing.
	agent, err := client.Agents.Create(ctx, agentbox.AgentCreateParams{
		Title:        "quickstart-" + time.Now().UTC().Format("20060102-150405"),
		ImageURL:     "docker.io/library/debian:bookworm-slim",
		Idc:          idc,
		InstanceType: instanceType,
		Runtime:      "sandbox",
	})
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		// Right after its sandboxes are deleted, the Agent can briefly report
		// its template busy (409); retry for a short while.
		for attempt := 0; attempt < 24; attempt++ {
			err := agent.Delete(ctx)
			if err == nil || !agentbox.IsConflictError(err) {
				if err != nil {
					log.Printf("delete agent: %v", err)
				}
				return
			}
			time.Sleep(5 * time.Second)
		}
		log.Printf("delete agent %s: still busy, delete it later", agent.Slug())
	}()

	if err := waitForTemplate(ctx, agent); err != nil {
		log.Print(err)
		return
	}
	fmt.Println("template ready for agent", agent.Slug())

	// 3. Launch a sandbox. TimeoutSeconds pins its lifetime, so it is
	// reclaimed even if this program dies before cleanup.
	timeout := 600
	sandbox, err := agent.Launch(ctx, agentbox.LaunchParams{
		InstanceType:   instanceType,
		TimeoutSeconds: &timeout,
	})
	if err != nil {
		log.Print(err)
		return
	}
	defer func() {
		if err := sandbox.Delete(ctx); err != nil {
			log.Printf("delete sandbox: %v", err)
		}
	}()
	if err := sandbox.WaitUntilRunning(ctx, agentbox.WaitUntilRunningParams{}); err != nil {
		log.Print(err)
		return
	}
	fmt.Println("sandbox", sandbox.ID(), "is", sandbox.Status(), "until", sandbox.ExpiresAt())

	// 4. Run a command and read its result.
	execution, err := sandbox.Execute(ctx, "uname -a && whoami", agentbox.ExecuteParams{})
	if err != nil {
		log.Print(err)
		return
	}
	exitCode := -1
	if code := execution.ExitCode(); code != nil {
		exitCode = *code
	}
	fmt.Printf("exec status=%s exit=%d stdout=%q\n", execution.Status(), exitCode, execution.Data["stdout"])

	// 5. Upload a file, change it inside the sandbox, download the result.
	const remote = "/home/user/hello.txt"
	if _, err := sandbox.UploadFile(ctx, remote, "", bytes.NewReader([]byte("hello from the SDK\n"))); err != nil {
		log.Print(err)
		return
	}
	if _, err := sandbox.Execute(ctx, "echo 'and from the sandbox' >> "+remote, agentbox.ExecuteParams{}); err != nil {
		log.Print(err)
		return
	}
	download, err := sandbox.DownloadFile(ctx, remote)
	if err != nil {
		log.Print(err)
		return
	}
	fmt.Printf("downloaded %s:\n%s", remote, download.Content)
}

// pickPlacement uses GMI_AGENTBOX_IDC and GMI_AGENTBOX_INSTANCE_TYPE when
// set, otherwise the first sandbox data center that lists a SKU.
func pickPlacement(ctx context.Context, client *agentbox.Client) (string, string, error) {
	idc, instanceType := os.Getenv("GMI_AGENTBOX_IDC"), os.Getenv("GMI_AGENTBOX_INSTANCE_TYPE")
	if idc != "" && instanceType != "" {
		return idc, instanceType, nil
	}
	idcs, err := client.Idcs.List(ctx, "sandbox")
	if err != nil {
		return "", "", err
	}
	for _, candidate := range idcs {
		if idc != "" && candidate.IdcID() != idc {
			continue
		}
		products, err := client.Products.List(ctx, agentbox.ProductListParams{IdcName: candidate.IdcID(), Runtime: "sandbox"})
		if err != nil || len(products) == 0 {
			continue // listed, but no SKU available right now
		}
		if instanceType == "" {
			instanceType = products[0].InstanceType()
		}
		return candidate.IdcID(), instanceType, nil
	}
	return "", "", fmt.Errorf("no sandbox data center with an available SKU")
}

// waitForTemplate polls until the Agent's template build finishes.
func waitForTemplate(ctx context.Context, agent *agentbox.Agent) error {
	deadline := time.Now().Add(15 * time.Minute)
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
			return fmt.Errorf("template still %q", agent.TemplateBuildStatus())
		}
		time.Sleep(3 * time.Second)
	}
}
