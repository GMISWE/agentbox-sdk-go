// Command long-running-exec runs commands that outlast a synchronous wait.
//
// A synchronous Execute waits at most about 25 seconds, whatever
// WaitTimeoutSeconds asks for; a command still running by then comes back
// with Accepted=true and status "running". For anything that can take longer,
// start it with Wait=false and poll GetExecution until the status is terminal.
// Results from GetExecution always have Accepted=false, so decide on Status,
// never on Accepted.
//
// It creates billable resources and deletes them before it exits.
//
//	go run ./examples/long-running-exec
package main

import (
	"context"
	"fmt"
	"log"
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
	agent, err := exampleutil.CreateAgent(ctx, client, cfg, exampleutil.UniqueTitle("long-exec"))
	if err != nil {
		return err
	}
	defer exampleutil.DeleteAgent(ctx, agent)

	sandbox, err := agent.Launch(ctx, agentbox.LaunchParams{
		InstanceType:   cfg.InstanceType,
		TimeoutSeconds: exampleutil.Ptr(900),
	})
	if err != nil {
		return err
	}
	defer func() { _ = sandbox.Delete(ctx) }()
	if err := sandbox.WaitUntilRunning(ctx, agentbox.WaitUntilRunningParams{}); err != nil {
		return err
	}

	// 1. A synchronous call that outlasts the wait returns while still running.
	start := time.Now()
	syncExec, err := sandbox.Execute(ctx, "sleep 40; echo sync-done", agentbox.ExecuteParams{
		WaitTimeoutSeconds: exampleutil.Ptr(120), // more than the server honours
	})
	if err != nil {
		return err
	}
	fmt.Printf("sync call returned after %s: accepted=%v status=%s\n",
		time.Since(start).Round(time.Second), syncExec.Accepted, syncExec.Status())
	if syncExec.Accepted {
		final, err := waitForExecution(ctx, sandbox, syncExec.ID(), 2*time.Minute)
		if err != nil {
			return err
		}
		fmt.Printf("  ...finished later: status=%s stdout=%q\n", final.Status(), exampleutil.Stdout(final))
	}

	// 2. The recommended pattern: start asynchronously, then poll.
	job, err := sandbox.Execute(ctx, "for i in 1 2 3; do echo step $i; sleep 10; done", agentbox.ExecuteParams{
		Wait: exampleutil.Ptr(false),
	})
	if err != nil {
		return err
	}
	fmt.Println("async execution", job.ID(), "accepted:", job.Accepted)
	final, err := waitForExecution(ctx, sandbox, job.ID(), 5*time.Minute)
	if err != nil {
		return err
	}
	fmt.Printf("async execution finished: status=%s exit=%s stdout=%q\n",
		final.Status(), exitCode(final), exampleutil.Stdout(final))

	// 3. Cancel an execution that is no longer needed.
	stuck, err := sandbox.Execute(ctx, "sleep 600", agentbox.ExecuteParams{Wait: exampleutil.Ptr(false)})
	if err != nil {
		return err
	}
	time.Sleep(3 * time.Second)
	if _, err := sandbox.CancelExecution(ctx, stuck.ID()); err != nil {
		return err
	}
	cancelled, err := waitForExecution(ctx, sandbox, stuck.ID(), time.Minute)
	if err != nil {
		return err
	}
	fmt.Println("cancelled execution status:", cancelled.Status())
	return nil
}

// waitForExecution polls until the execution reaches a terminal status.
func waitForExecution(ctx context.Context, sandbox *agentbox.Sandbox, id string, timeout time.Duration) (*agentbox.Execution, error) {
	deadline := time.Now().Add(timeout)
	for {
		execution, err := sandbox.GetExecution(ctx, id)
		if err != nil {
			return nil, err
		}
		switch execution.Status() {
		case "pending", "running", "cancelling":
			// still in progress
		default:
			return execution, nil // succeeded, failed, cancelled, ...
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("execution %s still %s after %s", id, execution.Status(), timeout)
		}
		time.Sleep(2 * time.Second)
	}
}

func exitCode(e *agentbox.Execution) string {
	if code := e.ExitCode(); code != nil {
		return fmt.Sprint(*code)
	}
	return "none"
}
