// Command pause-resume pauses a sandbox and wakes it again.
//
// A paused sandbox stops consuming run time. Resume is a cold start: files on
// disk come back, except /tmp, which is cleared; running processes do not, and
// the start command is not re-run. Keep anything you need across a pause under
// the home directory, and restart background processes after resume. A paused
// sandbox not resumed by PausedExpireAt is reclaimed together with its disk.
//
// It creates billable resources and deletes them before it exits.
//
//	go run ./examples/pause-resume
package main

import (
	"context"
	"fmt"
	"log"
	"strings"
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
	agent, err := exampleutil.CreateAgent(ctx, client, cfg, exampleutil.UniqueTitle("pause-resume"))
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

	// State in three places: the home directory, /tmp, and a running process.
	setup := "echo kept > /home/user/state.txt; echo lost > /tmp/state.txt;" +
		" nohup sleep 3600 >/dev/null 2>&1 &"
	if _, err := sandbox.Execute(ctx, setup, agentbox.ExecuteParams{}); err != nil {
		return err
	}

	// 1. Pause, then poll until the sandbox reports paused.
	if err := sandbox.Pause(ctx); err != nil {
		return err
	}
	if err := waitForStatus(ctx, sandbox, "paused", 5*time.Minute); err != nil {
		return err
	}
	fmt.Println("paused; reclaimed if not resumed by", sandbox.PausedExpireAt())

	// 2. Resume. Without TimeoutSeconds the remaining run time is kept.
	if err := sandbox.Resume(ctx, agentbox.ResumeParams{}); err != nil {
		return err
	}
	if err := sandbox.WaitUntilRunning(ctx, agentbox.WaitUntilRunningParams{}); err != nil {
		return err
	}

	// 3. See what survived.
	check := "cat /home/user/state.txt 2>&1; cat /tmp/state.txt 2>&1;" +
		" pgrep -x sleep >/dev/null && echo process: alive || echo process: gone"
	out, err := sandbox.Execute(ctx, check, agentbox.ExecuteParams{})
	if err != nil {
		return err
	}
	fmt.Println("after resume:")
	for _, line := range strings.Split(strings.TrimSpace(exampleutil.Stdout(out)), "\n") {
		fmt.Println("  " + line)
	}
	return nil
}

func waitForStatus(ctx context.Context, sandbox *agentbox.Sandbox, want string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if err := sandbox.Refresh(ctx); err != nil {
			return err
		}
		if sandbox.Status() == want {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("sandbox still %q after %s, want %q", sandbox.Status(), timeout, want)
		}
		time.Sleep(time.Second)
	}
}
