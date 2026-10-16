// Package exampleutil holds the setup shared by the examples: configuration,
// catalogue discovery, and creating an Agent whose template is ready.
//
// examples/quickstart spells every one of these steps out; the other examples
// call this package so they can focus on their own topic.
package exampleutil

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/GMISWE/agentbox-sdk-go/agentbox"
)

// DefaultImage is used when GMI_AGENTBOX_IMAGE is unset. Sandbox commands run
// through bash, so the image must provide it.
const DefaultImage = "docker.io/library/debian:bookworm-slim"

// Config is where and what to launch.
type Config struct {
	Idc          string
	InstanceType string
	Image        string
}

// LoadConfig reads GMI_AGENTBOX_IDC, GMI_AGENTBOX_INSTANCE_TYPE and
// GMI_AGENTBOX_IMAGE. When the IDC or SKU is unset, it picks the first sandbox
// data center that lists at least one sandbox SKU.
func LoadConfig(ctx context.Context, c *agentbox.Client) (Config, error) {
	cfg := Config{
		Idc:          os.Getenv("GMI_AGENTBOX_IDC"),
		InstanceType: os.Getenv("GMI_AGENTBOX_INSTANCE_TYPE"),
		Image:        os.Getenv("GMI_AGENTBOX_IMAGE"),
	}
	if cfg.Image == "" {
		cfg.Image = DefaultImage
	}
	if cfg.Idc != "" && cfg.InstanceType != "" {
		return cfg, nil
	}

	idcs, err := c.Idcs.List(ctx, "sandbox")
	if err != nil {
		return cfg, fmt.Errorf("list sandbox data centers: %w", err)
	}
	for _, idc := range idcs {
		if cfg.Idc != "" && idc.IdcID() != cfg.Idc {
			continue
		}
		// A data center can be listed while its catalogue is unavailable;
		// skip it rather than fail.
		products, err := c.Products.List(ctx, agentbox.ProductListParams{IdcName: idc.IdcID(), Runtime: "sandbox"})
		if err != nil || len(products) == 0 {
			continue
		}
		cfg.Idc = idc.IdcID()
		if cfg.InstanceType == "" {
			cfg.InstanceType = products[0].InstanceType()
		}
		return cfg, nil
	}
	return cfg, fmt.Errorf("no sandbox data center with an available SKU; set GMI_AGENTBOX_IDC and GMI_AGENTBOX_INSTANCE_TYPE")
}

// UniqueTitle returns prefix plus a UTC timestamp, so reruns do not collide.
func UniqueTitle(prefix string) string {
	return prefix + "-" + time.Now().UTC().Format("20060102-150405")
}

// CreateAgent registers a sandbox Agent and waits until its template is ready.
// The caller owns the returned Agent and must delete it.
func CreateAgent(ctx context.Context, c *agentbox.Client, cfg Config, title string) (*agentbox.Agent, error) {
	agent, err := c.Agents.Create(ctx, agentbox.AgentCreateParams{
		Title:        title,
		ImageURL:     cfg.Image,
		Idc:          cfg.Idc,
		InstanceType: cfg.InstanceType,
		Runtime:      "sandbox",
	})
	if err != nil {
		return nil, fmt.Errorf("create agent: %w", err)
	}
	if err := WaitForTemplate(ctx, agent, 15*time.Minute); err != nil {
		_ = agent.Delete(ctx)
		return nil, err
	}
	return agent, nil
}

// WaitForTemplate polls until the Agent's sandbox template build finishes.
// A new image can take a few minutes to build.
func WaitForTemplate(ctx context.Context, agent *agentbox.Agent, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
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
			return fmt.Errorf("template still %q after %s", agent.TemplateBuildStatus(), timeout)
		}
		time.Sleep(3 * time.Second)
	}
}

// Ptr returns a pointer to v, for optional parameters such as
// LaunchParams.TimeoutSeconds and ExecuteParams.Wait.
func Ptr[T any](v T) *T { return &v }

// Stdout returns an execution's captured stdout.
func Stdout(e *agentbox.Execution) string {
	s, _ := e.Data["stdout"].(string)
	return s
}

// DeleteAgent deletes an Agent, retrying while the service still reports its
// template busy (HTTP 409), which can happen right after its sandboxes or
// snapshots were deleted.
func DeleteAgent(ctx context.Context, agent *agentbox.Agent) {
	deadline := time.Now().Add(2 * time.Minute)
	for {
		err := agent.Delete(ctx)
		if err == nil || agentbox.IsNotFoundError(err) {
			return
		}
		if !agentbox.IsConflictError(err) || time.Now().After(deadline) {
			log.Printf("delete agent %s: %v", agent.Slug(), err)
			return
		}
		time.Sleep(5 * time.Second)
	}
}
