// Command large-output shows how to get a command's full output when it can
// exceed what Execute returns.
//
// Execute returns at most 1 MiB of stdout and 1 MiB of stderr. Output past
// that is dropped (the beginning is kept, the end is lost) and
// stdout_truncated / stderr_truncated are set to true in Execution.Data.
// Agent transcripts and build logs routinely pass 1 MiB, and their most
// useful lines are at the end, so redirect them to a file and download it.
//
// It creates billable resources and deletes them before it exits.
//
//	go run ./examples/large-output
package main

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/GMISWE/agentbox-sdk-go/agentbox"
	"github.com/GMISWE/agentbox-sdk-go/examples/internal/exampleutil"
)

// noisyCommand prints about 3 MiB and then the line that matters.
const noisyCommand = `seq 1 35000 | sed 's/^/log line: xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx /'; echo "RESULT: ok"`

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
	agent, err := exampleutil.CreateAgent(ctx, client, cfg, exampleutil.UniqueTitle("large-output"))
	if err != nil {
		return err
	}
	defer exampleutil.DeleteAgent(ctx, agent)

	sandbox, err := agent.Launch(ctx, agentbox.LaunchParams{
		InstanceType:   cfg.InstanceType,
		TimeoutSeconds: exampleutil.Ptr(600),
	})
	if err != nil {
		return err
	}
	defer func() { _ = sandbox.Delete(ctx) }()
	if err := sandbox.WaitUntilRunning(ctx, agentbox.WaitUntilRunningParams{}); err != nil {
		return err
	}

	// 1. Reading stdout directly: the tail is lost.
	direct, err := sandbox.Execute(ctx, noisyCommand, agentbox.ExecuteParams{})
	if err != nil {
		return err
	}
	stdout := exampleutil.Stdout(direct)
	fmt.Printf("direct: %d bytes, truncated=%v, has RESULT line=%v\n",
		len(stdout), Truncated(direct, "stdout"), strings.Contains(stdout, "RESULT:"))

	// 2. Redirect to a file, keep stdout small, and download the file.
	const logPath = "/home/user/run/output.log"
	toFile, err := sandbox.Execute(ctx,
		"mkdir -p /home/user/run && ("+noisyCommand+") > "+logPath+" 2>&1; echo exit=$?",
		agentbox.ExecuteParams{})
	if err != nil {
		return err
	}
	fmt.Printf("to file: stdout=%q\n", strings.TrimSpace(exampleutil.Stdout(toFile)))

	download, err := sandbox.DownloadFile(ctx, logPath)
	if err != nil {
		return err
	}
	full := string(download.Content)
	fmt.Printf("downloaded: %d bytes, last line=%q\n", len(full), lastLine(full))

	// 3. When only the end matters, read just the tail.
	tail, err := sandbox.Execute(ctx, "tail -n 1 "+logPath, agentbox.ExecuteParams{})
	if err != nil {
		return err
	}
	fmt.Printf("tail only: %q\n", strings.TrimSpace(exampleutil.Stdout(tail)))
	return nil
}

// Truncated reports whether Execute dropped part of a stream ("stdout" or
// "stderr").
func Truncated(e *agentbox.Execution, stream string) bool {
	v, _ := e.Data[stream+"_truncated"].(bool)
	return v
}

func lastLine(s string) string {
	s = strings.TrimRight(s, "\n")
	return s[strings.LastIndex(s, "\n")+1:]
}
