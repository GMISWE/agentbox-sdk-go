// Command labels-and-cleanup tags sandboxes with metadata, finds them again by
// tag, and deletes exactly those.
//
// Batch jobs and tests that launch many sandboxes should label every one of
// them with a run ID and delete by that label, never by status: other people
// in the same organization may have sandboxes running. A pinned
// TimeoutSeconds is the safety net when cleanup never runs.
//
// It creates billable resources and deletes them before it exits.
//
//	go run ./examples/labels-and-cleanup
package main

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"time"

	"github.com/GMISWE/agentbox-sdk-go/agentbox"
	"github.com/GMISWE/agentbox-sdk-go/examples/internal/exampleutil"
)

func main() {
	if err := run(context.Background()); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context) error {
	client, err := agentbox.NewClient()
	if err != nil {
		return err
	}
	cfg, err := exampleutil.LoadConfig(ctx, client)
	if err != nil {
		return err
	}
	agent, err := exampleutil.CreateAgent(ctx, client, cfg, exampleutil.UniqueTitle("labels"))
	if err != nil {
		return err
	}
	defer exampleutil.DeleteAgent(ctx, agent)

	runID := "run-" + time.Now().UTC().Format("20060102-150405")
	labels := map[string]string{"run": runID}

	// 1. Launch a few labelled sandboxes with a pinned lifetime.
	for i := 0; i < 3; i++ {
		sb, err := agent.Launch(ctx, agentbox.LaunchParams{
			InstanceType:   cfg.InstanceType,
			DisplayName:    fmt.Sprintf("%s-%d", runID, i),
			Metadata:       map[string]string{"run": runID, "role": "worker", "index": strconv.Itoa(i)},
			TimeoutSeconds: exampleutil.Ptr(600), // reclaimed after 10 minutes no matter what
		})
		if err != nil {
			return err
		}
		if err := sb.WaitUntilRunning(ctx, agentbox.WaitUntilRunningParams{}); err != nil {
			return err
		}
		fmt.Printf("launched %s labels=%v expires=%s pinned=%v\n", sb.ID(), sb.Metadata(), sb.ExpiresAt(), sb.ExpiryPinned())
	}

	// 2. Find them again by label. Keys are ANDed, values match exactly.
	page, err := client.Sandboxes.List(ctx, agentbox.SandboxListParams{Metadata: labels, PageSize: 100})
	if err != nil {
		return err
	}
	fmt.Printf("found %d sandboxes with run=%s\n", page.Total, runID)

	// A long job can extend (or shorten) one sandbox's lifetime.
	if len(page.Items) > 0 {
		if err := page.Items[0].SetTimeout(ctx, 1200); err != nil {
			return err
		}
		fmt.Println("extended", page.Items[0].ID(), "to", page.Items[0].ExpiresAt())
	}

	// 3. Delete exactly what this run created.
	for _, sb := range page.Items {
		if sb.Metadata()["run"] != runID { // belt and braces
			continue
		}
		if err := sb.Delete(ctx); err != nil && !agentbox.IsNotFoundError(err) {
			return err
		}
	}

	// 4. Confirm nothing from this run is left. The default list already
	// excludes stopped and deleted sandboxes.
	page, err = client.Sandboxes.List(ctx, agentbox.SandboxListParams{Metadata: labels, PageSize: 100})
	if err != nil {
		return err
	}
	fmt.Printf("left over after cleanup: %d\n", page.Total)
	return nil
}
