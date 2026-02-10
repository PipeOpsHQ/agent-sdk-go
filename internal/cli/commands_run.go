package cli

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/PipeOpsHQ/agent-sdk-go/framework/state"
	statefactory "github.com/PipeOpsHQ/agent-sdk-go/framework/state/factory"
)

func runSingle(ctx context.Context, args []string) {
	opts, positional := parseArgs(args)
	input := normalizeInput(positional)
	if input == "" {
		log.Fatal("input cannot be empty")
	}

	provider, store := buildRuntimeDeps(ctx)
	defer closeStore(store)
	observer, closeObserver := buildObserver()
	defer closeObserver()

	agent, err := buildAgent(provider, store, observer, opts)
	if err != nil {
		log.Fatalf("failed to create agent: %v", err)
	}
	output, err := agent.Run(ctx, input)
	if err != nil {
		log.Fatalf("run failed: %v", err)
	}
	fmt.Println(output)
}

func runGraph(ctx context.Context, args []string) {
	opts, positional := parseArgs(args)
	input := normalizeInput(positional)
	if input == "" {
		log.Fatal("input cannot be empty")
	}

	provider, store := buildRuntimeDeps(ctx)
	defer closeStore(store)
	observer, closeObserver := buildObserver()
	defer closeObserver()

	agent, err := buildAgent(provider, nil, observer, opts)
	if err != nil {
		log.Fatalf("failed to create agent: %v", err)
	}
	exec, err := buildExecutor(agent, store, observer, opts)
	if err != nil {
		log.Fatalf("failed to create graph executor: %v", err)
	}
	result, err := exec.Run(ctx, input)
	if err != nil {
		log.Fatalf("graph run failed: %v", err)
	}
	fmt.Println(result.Output)
}

func resumeGraph(ctx context.Context, args []string) {
	opts, positional := parseArgs(args)
	if len(positional) < 1 {
		log.Fatal("usage: graph-resume [--workflow=name] [--tools=list] <run-id>")
	}
	runID := strings.TrimSpace(positional[0])
	if runID == "" {
		log.Fatal("run-id cannot be empty")
	}

	provider, store := buildRuntimeDeps(ctx)
	defer closeStore(store)
	observer, closeObserver := buildObserver()
	defer closeObserver()

	agent, err := buildAgent(provider, nil, observer, opts)
	if err != nil {
		log.Fatalf("failed to create agent: %v", err)
	}
	exec, err := buildExecutor(agent, store, observer, opts)
	if err != nil {
		log.Fatalf("failed to create graph executor: %v", err)
	}
	result, err := exec.Resume(ctx, runID)
	if err != nil {
		log.Fatalf("graph resume failed: %v", err)
	}
	fmt.Println(result.Output)
}

func listSessions(ctx context.Context, args []string) {
	sessionID := ""
	if len(args) > 0 {
		sessionID = strings.TrimSpace(args[0])
	}

	store, err := statefactory.FromEnv(ctx)
	if err != nil {
		log.Fatal(err)
	}
	defer closeStore(store)

	runs, err := store.ListRuns(ctx, state.ListRunsQuery{SessionID: sessionID, Limit: 100})
	if err != nil {
		log.Fatalf("list sessions failed: %v", err)
	}
	sort.Slice(runs, func(i, j int) bool {
		left, right := time.Time{}, time.Time{}
		if runs[i].UpdatedAt != nil {
			left = *runs[i].UpdatedAt
		}
		if runs[j].UpdatedAt != nil {
			right = *runs[j].UpdatedAt
		}
		return left.After(right)
	})
	for _, run := range runs {
		updated := "-"
		if run.UpdatedAt != nil {
			updated = run.UpdatedAt.UTC().Format(time.RFC3339)
		}
		fmt.Printf("%s\t%s\t%s\t%s\t%s\n", run.RunID, run.SessionID, run.Status, run.Provider, updated)
	}
}
