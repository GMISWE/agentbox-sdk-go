// Command shell opens an interactive shell in a sandbox over WebSocket.
//
// Beta: the shell API and its message protocol may change in a later release.
//
// The shell speaks JSON text frames: SendShellInput types into the shell, ResizeShell
// sets the terminal size, RecvShellMessage returns status, stdout and error
// messages. Output is raw terminal output (input echo, "\r\n", stderr merged);
// use Execute when you need clean output and exit codes.
//
// Disconnecting and reconnecting resumes the same shell session, so the
// working directory survives. Only one connection can be attached at a time.
//
// It creates billable resources and deletes them before it exits.
//
//	go run ./examples/shell
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
	agent, err := exampleutil.CreateAgent(ctx, client, cfg, exampleutil.UniqueTitle("shell"))
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

	// 1. First attach: change directory and print it.
	conn, err := sandbox.Shell(ctx)
	if err != nil {
		return err
	}
	if err := waitForStatus(conn, "connected"); err != nil {
		_ = conn.Close()
		return err
	}
	if err := agentbox.ResizeShell(conn, 120, 32); err != nil {
		_ = conn.Close()
		return err
	}
	if err := agentbox.SendShellInput(conn, "cd /tmp && echo FIRST:$(pwd)\n"); err != nil {
		_ = conn.Close()
		return err
	}
	out, err := readUntil(conn, "FIRST:/tmp", 30*time.Second)
	// Close the connection rather than running `exit`: after `exit` the
	// service closes the shell and later attaches to this sandbox are closed
	// immediately.
	_ = conn.Close()
	if err != nil {
		return err
	}
	fmt.Printf("first attach saw:  %q\n", out)

	// 2. Reconnect: the same session continues, so the directory is kept.
	conn, err = sandbox.Shell(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	if err := waitForStatus(conn, "connected"); err != nil {
		return err
	}
	if err := agentbox.SendShellInput(conn, "echo RESUMED:$(pwd)\n"); err != nil {
		return err
	}
	out, err = readUntil(conn, "RESUMED:/tmp", 30*time.Second)
	if err != nil {
		return err
	}
	fmt.Printf("after reconnect:   %q\n", out)
	return nil
}

// waitForStatus reads messages until a status message with the given text.
func waitForStatus(conn agentbox.ShellConn, want string) error {
	for {
		m, err := agentbox.RecvShellMessage(conn)
		if err != nil {
			return err
		}
		switch {
		case m.Type == "status" && m.Message == want:
			return nil
		case m.Type == "error":
			return fmt.Errorf("shell error: %s", m.Message)
		}
	}
}

// readUntil collects stdout until it contains marker, or fails after timeout.
// The marker is the printed form (for example "RESUMED:/tmp"). The echo of the
// typed command contains "RESUMED:$(pwd)" literally, so neither echoed input
// nor replayed scrollback from an earlier command can match it.
func readUntil(conn agentbox.ShellConn, marker string, timeout time.Duration) (string, error) {
	type result struct {
		out string
		err error
	}
	done := make(chan result, 1)
	go func() {
		var buf strings.Builder
		for {
			m, err := agentbox.RecvShellMessage(conn)
			if err != nil {
				done <- result{buf.String(), err}
				return
			}
			switch m.Type {
			case "stdout":
				buf.WriteString(m.Data)
				if strings.Contains(buf.String(), marker) {
					done <- result{buf.String(), nil}
					return
				}
			case "error":
				done <- result{buf.String(), fmt.Errorf("shell error: %s", m.Message)}
				return
			}
		}
	}()
	select {
	case r := <-done:
		return r.out, r.err
	case <-time.After(timeout):
		return "", fmt.Errorf("no %q in shell output after %s", marker, timeout)
	}
}
