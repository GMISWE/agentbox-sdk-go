// Command snapshot-pool prepares one sandbox, saves it as a snapshot, and
// launches several identical sandboxes from that snapshot in parallel.
//
// Installing tools once and launching from a snapshot is much faster than
// installing them in every sandbox. Clean out caches, credentials and agent
// state before the snapshot: every sandbox launched from it starts with that
// exact disk. Environment variables passed at launch are not part of the
// snapshot; pass them again on each launch.
//
// It creates billable resources and deletes them before it exits.
//
//	POOL_SIZE=5 go run ./examples/snapshot-pool
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
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
	poolSize := 5
	if n, err := strconv.Atoi(os.Getenv("POOL_SIZE")); err == nil && n > 0 {
		poolSize = n
	}

	client, err := agentbox.NewClient()
	if err != nil {
		return err
	}
	cfg, err := exampleutil.LoadConfig(ctx, client)
	if err != nil {
		return err
	}
	agent, err := exampleutil.CreateAgent(ctx, client, cfg, exampleutil.UniqueTitle("snapshot-pool"))
	if err != nil {
		return err
	}
	defer exampleutil.DeleteAgent(ctx, agent)

	// 1. Prepare a base sandbox: install what every worker needs.
	base, err := agent.Launch(ctx, agentbox.LaunchParams{
		InstanceType:   cfg.InstanceType,
		DisplayName:    "pool-base",
		TimeoutSeconds: exampleutil.Ptr(900),
	})
	if err != nil {
		return err
	}
	defer func() { _ = base.Delete(ctx) }()
	if err := base.WaitUntilRunning(ctx, agentbox.WaitUntilRunningParams{}); err != nil {
		return err
	}
	prepare := "mkdir -p /home/user/tools && echo 'v1' > /home/user/tools/VERSION" +
		" && rm -rf /home/user/.cache" // drop anything a worker should not inherit
	if _, err := base.Execute(ctx, prepare, agentbox.ExecuteParams{}); err != nil {
		return err
	}

	// 2. Snapshot it. Only a running sandbox can be saved.
	snapshot, err := base.CreateSnapshot(ctx, agentbox.CreateSnapshotParams{Name: "pool-base-v1"})
	if err != nil {
		return err
	}
	defer func() { _ = snapshot.Delete(ctx) }()
	if err := snapshot.WaitUntilReady(ctx, agentbox.WaitUntilReadyParams{Timeout: 10 * time.Minute}); err != nil {
		return err
	}
	fmt.Printf("snapshot %s ready (%d vCPU, %d MB RAM)\n", snapshot.ID(), snapshot.VCPU(), snapshot.RAMMB())

	// 3. Launch the pool from the snapshot in parallel. Sizing and data center
	// come from the snapshot, so InstanceType is not needed.
	type result struct {
		sandbox *agentbox.Sandbox
		ready   time.Duration
		err     error
	}
	results := make([]result, poolSize)
	var wg sync.WaitGroup
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			start := time.Now()
			sb, err := agent.Launch(ctx, agentbox.LaunchParams{
				SnapshotID:     snapshot.ID(),
				DisplayName:    fmt.Sprintf("pool-worker-%02d", i),
				Metadata:       map[string]string{"pool": snapshot.ID(), "worker": strconv.Itoa(i)},
				TimeoutSeconds: exampleutil.Ptr(600),
			})
			if err == nil {
				err = sb.WaitUntilRunning(ctx, agentbox.WaitUntilRunningParams{PollInterval: 500 * time.Millisecond})
			}
			results[i] = result{sandbox: sb, ready: time.Since(start), err: err}
		}(i)
	}
	wg.Wait()

	var ready []time.Duration
	for i, r := range results {
		if r.sandbox != nil {
			defer func(sb *agentbox.Sandbox) { _ = sb.Delete(ctx) }(r.sandbox)
		}
		if r.err != nil {
			fmt.Printf("worker %02d failed: %v\n", i, r.err)
			continue
		}
		ready = append(ready, r.ready)
		check, err := r.sandbox.Execute(ctx, "cat /home/user/tools/VERSION", agentbox.ExecuteParams{})
		if err != nil {
			fmt.Printf("worker %02d exec failed: %v\n", i, err)
			continue
		}
		fmt.Printf("worker %02d running in %s, tools version %s\n",
			i, r.ready.Round(time.Millisecond), strings.TrimSpace(exampleutil.Stdout(check)))
	}
	if len(ready) > 0 {
		sort.Slice(ready, func(a, b int) bool { return ready[a] < ready[b] })
		fmt.Printf("%d/%d running; median %s, slowest %s\n", len(ready), poolSize,
			ready[len(ready)/2].Round(time.Millisecond), ready[len(ready)-1].Round(time.Millisecond))
	}
	return nil
}
