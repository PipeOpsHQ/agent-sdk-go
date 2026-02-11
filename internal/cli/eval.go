package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	evalfw "github.com/PipeOpsHQ/agent-sdk-go/eval"
	"github.com/PipeOpsHQ/agent-sdk-go/observe"
)

type evalCLIOptions struct {
	dataset      string
	output       string
	failUnder    float64
	maxCases     int
	workers      int
	retries      int
	retryBackoff time.Duration
	caseTimeout  time.Duration
	timeout      time.Duration
	judgeRubric  string
	judgeMin     float64
	judgeEnabled bool
	agentOpts    []string
}

func runEvalCLI(ctx context.Context, args []string) {
	opts := parseEvalArgs(args)
	if strings.TrimSpace(opts.dataset) == "" {
		log.Fatal("usage: eval --dataset=path/to/file.jsonl [--output=markdown|json] [--fail-under=100] [--max-cases=50] [--workers=4] [--retries=1] [--case-timeout-ms=45000] [--timeout-ms=300000]")
	}

	dataset, err := evalfw.LoadJSONL(opts.dataset)
	if err != nil {
		log.Fatalf("failed to load dataset: %v", err)
	}

	provider, store := buildRuntimeDeps(ctx)
	defer closeStore(store)

	parsedAgentOpts, _ := parseArgs(opts.agentOpts)
	agent, err := buildAgent(provider, nil, observe.NoopSink{}, parsedAgentOpts)
	if err != nil {
		log.Fatalf("failed to create agent: %v", err)
	}

	var judge evalfw.Judge
	if opts.judgeEnabled {
		j, err := evalfw.NewLLMJudge(provider)
		if err != nil {
			log.Fatalf("failed to create judge: %v", err)
		}
		judge = j
	}

	runner, err := evalfw.NewRunner(evalfw.RunnerConfig{Agent: agent, Judge: judge})
	if err != nil {
		log.Fatalf("failed to create eval runner: %v", err)
	}

	report, err := runner.Run(ctx, dataset, evalfw.RunOptions{
		DatasetPath:   opts.dataset,
		Provider:      provider.Name(),
		MaxCases:      opts.maxCases,
		Workers:       opts.workers,
		Retries:       opts.retries,
		RetryBackoff:  opts.retryBackoff,
		CaseTimeout:   opts.caseTimeout,
		Timeout:       opts.timeout,
		JudgeRubric:   opts.judgeRubric,
		MinJudgeScore: opts.judgeMin,
	})
	if err != nil {
		log.Fatalf("eval run failed: %v", err)
	}

	output := strings.ToLower(strings.TrimSpace(opts.output))
	switch output {
	case "", "markdown", "md":
		fmt.Println(evalfw.FormatMarkdown(report))
	case "json":
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			log.Fatalf("failed to encode report: %v", err)
		}
		fmt.Println(string(data))
	default:
		log.Fatalf("unsupported output format %q (use markdown or json)", output)
	}

	if report.PassRate < opts.failUnder {
		log.Fatalf("eval failed threshold: pass rate %.2f%% is below fail-under %.2f%%", report.PassRate, opts.failUnder)
	}
}

func parseEvalArgs(args []string) evalCLIOptions {
	opts := evalCLIOptions{
		output:       "markdown",
		failUnder:    100,
		workers:      4,
		retries:      1,
		retryBackoff: 400 * time.Millisecond,
		judgeMin:     0.7,
	}
	for _, arg := range args {
		switch {
		case strings.HasPrefix(arg, "--dataset="):
			opts.dataset = strings.TrimSpace(strings.TrimPrefix(arg, "--dataset="))
		case strings.HasPrefix(arg, "--output="):
			opts.output = strings.TrimSpace(strings.TrimPrefix(arg, "--output="))
		case strings.HasPrefix(arg, "--fail-under="):
			raw := strings.TrimSpace(strings.TrimPrefix(arg, "--fail-under="))
			if v, err := strconv.ParseFloat(raw, 64); err == nil && v >= 0 && v <= 100 {
				opts.failUnder = v
			}
		case strings.HasPrefix(arg, "--max-cases="):
			raw := strings.TrimSpace(strings.TrimPrefix(arg, "--max-cases="))
			if v, err := strconv.Atoi(raw); err == nil && v > 0 {
				opts.maxCases = v
			}
		case strings.HasPrefix(arg, "--workers="):
			raw := strings.TrimSpace(strings.TrimPrefix(arg, "--workers="))
			if v, err := strconv.Atoi(raw); err == nil && v > 0 {
				opts.workers = v
			}
		case strings.HasPrefix(arg, "--retries="):
			raw := strings.TrimSpace(strings.TrimPrefix(arg, "--retries="))
			if v, err := strconv.Atoi(raw); err == nil && v >= 0 {
				opts.retries = v
			}
		case strings.HasPrefix(arg, "--retry-backoff-ms="):
			raw := strings.TrimSpace(strings.TrimPrefix(arg, "--retry-backoff-ms="))
			if v, err := strconv.Atoi(raw); err == nil && v > 0 {
				opts.retryBackoff = time.Duration(v) * time.Millisecond
			}
		case strings.HasPrefix(arg, "--case-timeout-ms="):
			raw := strings.TrimSpace(strings.TrimPrefix(arg, "--case-timeout-ms="))
			if v, err := strconv.Atoi(raw); err == nil && v > 0 {
				opts.caseTimeout = time.Duration(v) * time.Millisecond
			}
		case strings.HasPrefix(arg, "--timeout-ms="):
			raw := strings.TrimSpace(strings.TrimPrefix(arg, "--timeout-ms="))
			if v, err := strconv.Atoi(raw); err == nil && v > 0 {
				opts.timeout = time.Duration(v) * time.Millisecond
			}
		case strings.HasPrefix(arg, "--judge-rubric="):
			opts.judgeRubric = strings.TrimSpace(strings.TrimPrefix(arg, "--judge-rubric="))
		case strings.HasPrefix(arg, "--judge-min-score="):
			raw := strings.TrimSpace(strings.TrimPrefix(arg, "--judge-min-score="))
			if v, err := strconv.ParseFloat(raw, 64); err == nil && v >= 0 && v <= 1 {
				opts.judgeMin = v
			}
		case arg == "--judge", arg == "--judge=true":
			opts.judgeEnabled = true
		case arg == "--judge=false":
			opts.judgeEnabled = false
		case strings.HasPrefix(arg, "--tools="),
			strings.HasPrefix(arg, "--system-prompt="),
			strings.HasPrefix(arg, "--prompt-template="):
			opts.agentOpts = append(opts.agentOpts, arg)
		}
	}
	return opts
}
