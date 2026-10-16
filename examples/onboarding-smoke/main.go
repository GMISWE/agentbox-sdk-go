// Command onboarding-smoke verifies that an organization has been onboarded
// to AgentBox: the sandbox runtime is enabled for the data center, an Agent's
// template builds, and a sandbox starts. Run it with an API key that belongs
// to the organization being checked (created under Compute, not Inference).
//
// Passing means the organization can create sandboxes. If the organization
// has no credit yet, billing stops the sandbox a few seconds after it starts;
// that is expected and does not fail the check.
//
// It creates billable resources (named gmi-smoke-*) and deletes them before it
// exits, including on failure.
//
//	export GMI_AGENTBOX_API_KEY=...            # a key of the organization under test
//	export GMI_AGENTBOX_IDC=us-central-iowa16  # optional; this is the default
//	export GMI_AGENTBOX_INSTANCE_TYPE=gmi.sandbox.small  # optional; this is the default
//	go run ./examples/onboarding-smoke
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"slices"
	"time"

	"github.com/GMISWE/agentbox-sdk-go/agentbox"
	"github.com/GMISWE/agentbox-sdk-go/examples/internal/exampleutil"
)

func main() {
	if err := run(context.Background()); err != nil {
		fmt.Println("RESULT: FAIL")
		log.Fatal(err)
	}
	fmt.Println("RESULT: PASS")
}

func run(ctx context.Context) error {
	idc := envOr("GMI_AGENTBOX_IDC", "us-central-iowa16")
	instanceType := envOr("GMI_AGENTBOX_INSTANCE_TYPE", "gmi.sandbox.small")

	client, err := agentbox.NewClient()
	if err != nil {
		return err
	}

	// 1. Eligibility: the sandbox runtime is enabled and lists the data center.
	// It reflects the billing whitelist, not the balance.
	eligibility, err := client.Eligibility(ctx)
	if err != nil {
		return fmt.Errorf("eligibility: %w", err)
	}
	available, dataCenters := sandboxEligibility(eligibility)
	if !available || !slices.Contains(dataCenters, idc) {
		return fmt.Errorf("eligibility: sandbox not enabled for %s (available=%v, data centers=%v)", idc, available, dataCenters)
	}
	fmt.Println("PASS eligibility, sandbox data centers:", dataCenters)

	// 2. Register an Agent and wait for its template.
	cfg := exampleutil.Config{Idc: idc, InstanceType: instanceType, Image: exampleutil.DefaultImage}
	agent, err := exampleutil.CreateAgent(ctx, client, cfg, exampleutil.UniqueTitle("gmi-smoke"))
	if err != nil {
		return err
	}
	defer exampleutil.DeleteAgent(ctx, agent)
	fmt.Println("PASS agent and template:", agent.Slug())

	// 3. Launch a sandbox and wait for it to run.
	sandbox, err := agent.Launch(ctx, agentbox.LaunchParams{
		InstanceType:   instanceType,
		TimeoutSeconds: exampleutil.Ptr(600),
		Metadata:       map[string]string{"purpose": "gmi-smoke"},
	})
	if err != nil {
		return fmt.Errorf("launch: %w", err)
	}
	defer func() { _ = sandbox.Delete(ctx) }()
	if err := sandbox.WaitUntilRunning(ctx, agentbox.WaitUntilRunningParams{PollInterval: time.Second}); err != nil {
		return fmt.Errorf("sandbox did not reach running: %w", err)
	}
	fmt.Println("PASS sandbox running:", sandbox.ID())
	fmt.Println("note: without credit, billing stops this sandbox within seconds; that is expected")
	return nil
}

// sandboxEligibility reads runtimes.sandbox from the eligibility response.
func sandboxEligibility(e *agentbox.Eligibility) (bool, []string) {
	runtimes, _ := e.Data["runtimes"].(map[string]any)
	sandbox, _ := runtimes["sandbox"].(map[string]any)
	available, _ := sandbox["available"].(bool)
	var dataCenters []string
	list, _ := sandbox["dataCenters"].([]any)
	for _, dc := range list {
		dataCenters = append(dataCenters, fmt.Sprint(dc))
	}
	return available, dataCenters
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
