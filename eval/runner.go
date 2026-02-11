package eval

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/PipeOpsHQ/agent-sdk-go/types"
)

type Agent interface {
	RunDetailed(ctx context.Context, input string) (types.RunResult, error)
}

type Runner struct {
	agent Agent
	judge Judge
}

type RunnerConfig struct {
	Agent Agent
	Judge Judge
}

type RunOptions struct {
	DatasetPath   string
	Provider      string
	MaxCases      int
	Workers       int
	Retries       int
	RetryBackoff  time.Duration
	CaseTimeout   time.Duration
	Timeout       time.Duration
	JudgeRubric   string
	MinJudgeScore float64
}

type Report struct {
	Dataset                string                `json:"dataset,omitempty"`
	Provider               string                `json:"provider,omitempty"`
	StartedAt              time.Time             `json:"startedAt"`
	CompletedAt            time.Time             `json:"completedAt"`
	Total                  int                   `json:"total"`
	Passed                 int                   `json:"passed"`
	Failed                 int                   `json:"failed"`
	PassRate               float64               `json:"passRate"`
	AvgLatencyMs           float64               `json:"avgLatencyMs"`
	LatencyP50Ms           int64                 `json:"latencyP50Ms"`
	LatencyP95Ms           int64                 `json:"latencyP95Ms"`
	TotalInputTokens       int                   `json:"totalInputTokens"`
	TotalOutputTokens      int                   `json:"totalOutputTokens"`
	TotalTokens            int                   `json:"totalTokens"`
	ToolConstraintCases    int                   `json:"toolConstraintCases"`
	ToolConstraintPassed   int                   `json:"toolConstraintPassed"`
	ToolConstraintAccuracy float64               `json:"toolConstraintAccuracy"`
	PerTag                 map[string]TagMetrics `json:"perTag,omitempty"`
	Results                []CaseResult          `json:"results"`
}

type TagMetrics struct {
	Total    int     `json:"total"`
	Passed   int     `json:"passed"`
	Failed   int     `json:"failed"`
	PassRate float64 `json:"passRate"`
}

type CaseResult struct {
	CaseID    string         `json:"caseId"`
	Input     string         `json:"input,omitempty"`
	Output    string         `json:"output,omitempty"`
	Tags      []string       `json:"tags,omitempty"`
	Pass      bool           `json:"pass"`
	Error     string         `json:"error,omitempty"`
	LatencyMs int64          `json:"latencyMs"`
	Usage     *types.Usage   `json:"usage,omitempty"`
	UsedTools []string       `json:"usedTools,omitempty"`
	Checks    []CheckResult  `json:"checks"`
	Metadata  map[string]any `json:"metadata,omitempty"`
	Attempts  int            `json:"attempts,omitempty"`
	Judge     *JudgeResult   `json:"judge,omitempty"`
}

func NewRunner(cfg RunnerConfig) (*Runner, error) {
	if cfg.Agent == nil {
		return nil, errors.New("runner agent is required")
	}
	return &Runner{agent: cfg.Agent, judge: cfg.Judge}, nil
}

func (r *Runner) Run(ctx context.Context, cases []Case, opts RunOptions) (Report, error) {
	if r == nil || r.agent == nil {
		return Report{}, errors.New("runner agent is required")
	}
	if len(cases) == 0 {
		return Report{}, errors.New("at least one case is required")
	}
	if opts.MaxCases > 0 && opts.MaxCases < len(cases) {
		cases = cases[:opts.MaxCases]
	}
	runCtx := ctx
	cancel := func() {}
	if opts.Timeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, opts.Timeout)
	}
	defer cancel()

	workers := opts.Workers
	if workers <= 0 {
		workers = defaultWorkers(len(cases))
	}
	if workers > len(cases) {
		workers = len(cases)
	}
	retries := opts.Retries
	if retries < 0 {
		retries = 0
	}
	backoff := opts.RetryBackoff
	if backoff <= 0 {
		backoff = 400 * time.Millisecond
	}

	report := Report{
		Dataset:   opts.DatasetPath,
		Provider:  opts.Provider,
		StartedAt: time.Now().UTC(),
		Results:   make([]CaseResult, 0, len(cases)),
		PerTag:    map[string]TagMetrics{},
	}

	results := make([]CaseResult, len(cases))
	type job struct {
		idx int
		c   Case
	}
	jobs := make(chan job)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				results[j.idx] = r.runCaseWithRetry(runCtx, j.c, opts, retries, backoff)
			}
		}()
	}
	dispatched := 0
dispatchLoop:
	for idx, c := range cases {
		select {
		case <-runCtx.Done():
			for i := idx; i < len(cases); i++ {
				results[i] = contextFailureResult(cases[i], runCtx.Err(), 0)
			}
			break dispatchLoop
		case jobs <- job{idx: idx, c: c}:
			dispatched++
		}
	}
	close(jobs)
	wg.Wait()
	if dispatched == 0 && len(cases) > 0 {
		for i := range cases {
			if results[i].CaseID == "" {
				results[i] = contextFailureResult(cases[i], runCtx.Err(), 0)
			}
		}
	}

	latencies := make([]int64, 0, len(cases))
	for _, res := range results {
		report.Results = append(report.Results, res)
		report.Total++
		if res.Pass {
			report.Passed++
		} else {
			report.Failed++
		}
		latencies = append(latencies, res.LatencyMs)
		if res.Usage != nil {
			report.TotalInputTokens += res.Usage.InputTokens
			report.TotalOutputTokens += res.Usage.OutputTokens
			report.TotalTokens += res.Usage.TotalTokens
		}

		for _, tag := range res.Tags {
			m := report.PerTag[tag]
			m.Total++
			if res.Pass {
				m.Passed++
			} else {
				m.Failed++
			}
			report.PerTag[tag] = m
		}

		hasToolConstraint := false
		for _, check := range res.Checks {
			if strings.HasPrefix(check.Name, "required_tool:") || strings.HasPrefix(check.Name, "forbidden_tool:") {
				hasToolConstraint = true
				break
			}
		}
		if hasToolConstraint {
			report.ToolConstraintCases++
			if toolChecksPass(res.Checks) {
				report.ToolConstraintPassed++
			}
		}
	}

	report.CompletedAt = time.Now().UTC()
	report.PassRate = ratio(report.Passed, report.Total)
	report.AvgLatencyMs = averageInt64(latencies)
	report.LatencyP50Ms = percentile(latencies, 50)
	report.LatencyP95Ms = percentile(latencies, 95)
	report.ToolConstraintAccuracy = ratio(report.ToolConstraintPassed, report.ToolConstraintCases)

	for tag, m := range report.PerTag {
		m.PassRate = ratio(m.Passed, m.Total)
		report.PerTag[tag] = m
	}

	return report, nil
}

func (r *Runner) runCaseWithRetry(ctx context.Context, c Case, runOpts RunOptions, retries int, backoff time.Duration) CaseResult {
	caseCtx := ctx
	cancel := func() {}
	if runOpts.CaseTimeout > 0 {
		caseCtx, cancel = context.WithTimeout(ctx, runOpts.CaseTimeout)
	}
	defer cancel()

	var last CaseResult
	attempts := retries + 1
	if attempts < 1 {
		attempts = 1
	}
	for attempt := 1; attempt <= attempts; attempt++ {
		if err := caseCtx.Err(); err != nil {
			failed := contextFailureResult(c, err, attempt-1)
			return failed
		}

		res := r.runCaseWithOptions(caseCtx, c, runOpts)
		res.Attempts = attempt
		last = res
		if strings.TrimSpace(res.Error) == "" {
			return res
		}
		if attempt < attempts {
			select {
			case <-caseCtx.Done():
				return last
			case <-time.After(backoffForAttempt(backoff, attempt)):
			}
		}
	}
	return last
}

func contextFailureResult(c Case, err error, attempts int) CaseResult {
	errText := "context canceled"
	if err != nil {
		errText = err.Error()
	}
	return CaseResult{
		CaseID:   c.ID,
		Input:    c.Input,
		Tags:     append([]string(nil), c.Tags...),
		Pass:     false,
		Error:    errText,
		Checks:   []CheckResult{{Name: "run", Pass: false, Detail: errText}},
		Attempts: attempts,
		Metadata: c.Metadata,
	}
}

func (r *Runner) runCaseWithOptions(ctx context.Context, c Case, runOpts RunOptions) CaseResult {
	caseStarted := time.Now()
	result := CaseResult{
		CaseID:   c.ID,
		Input:    c.Input,
		Tags:     append([]string(nil), c.Tags...),
		Checks:   make([]CheckResult, 0, 8),
		Metadata: c.Metadata,
	}

	runResult, err := r.agent.RunDetailed(ctx, c.Input)
	if err != nil {
		result.Error = err.Error()
		result.Pass = false
		result.LatencyMs = time.Since(caseStarted).Milliseconds()
		result.Checks = append(result.Checks, CheckResult{Name: "run", Pass: false, Detail: err.Error()})
		return result
	}

	result.Output = runResult.Output
	result.Usage = runResult.Usage
	result.UsedTools = extractUsedTools(runResult)
	result.LatencyMs = latencyFromRun(runResult, caseStarted)

	if strings.TrimSpace(c.ExpectedOutput) != "" {
		expected := c.ExpectedOutput
		if strings.Contains(result.Output, expected) {
			result.Checks = append(result.Checks, CheckResult{Name: "expected_output", Pass: true})
		} else {
			result.Checks = append(result.Checks, CheckResult{
				Name:   "expected_output",
				Pass:   false,
				Detail: fmt.Sprintf("output does not contain expected substring %q", expected),
			})
		}
	}

	for _, name := range c.RequiredTools {
		if containsString(result.UsedTools, name) {
			result.Checks = append(result.Checks, CheckResult{Name: "required_tool:" + name, Pass: true})
		} else {
			result.Checks = append(result.Checks, CheckResult{Name: "required_tool:" + name, Pass: false, Detail: "tool was not called"})
		}
	}

	for _, name := range c.ForbiddenTools {
		if containsString(result.UsedTools, name) {
			result.Checks = append(result.Checks, CheckResult{Name: "forbidden_tool:" + name, Pass: false, Detail: "forbidden tool was called"})
		} else {
			result.Checks = append(result.Checks, CheckResult{Name: "forbidden_tool:" + name, Pass: true})
		}
	}

	result.Checks = append(result.Checks, runAssertions(result.Output, c.Assertions)...)
	result = r.evaluateJudge(ctx, result, c, runOpts)
	result.Pass = allChecksPass(result.Checks)
	return result
}

func (r *Runner) evaluateJudge(ctx context.Context, result CaseResult, c Case, runOpts RunOptions) CaseResult {
	if r == nil || r.judge == nil {
		return result
	}
	rubric := strings.TrimSpace(c.JudgeRubric)
	if rubric == "" {
		rubric = strings.TrimSpace(runOpts.JudgeRubric)
	}
	if rubric == "" {
		return result
	}
	minScore := c.MinJudgeScore
	if minScore <= 0 {
		minScore = runOpts.MinJudgeScore
	}
	if minScore <= 0 {
		minScore = 0.7
	}

	score, err := r.judge.Score(ctx, JudgeInput{
		CaseID:         c.ID,
		Input:          c.Input,
		Expected:       c.ExpectedOutput,
		Output:         result.Output,
		Rubric:         rubric,
		Assertions:     c.Assertions,
		RequiredTools:  c.RequiredTools,
		ForbiddenTools: c.ForbiddenTools,
		UsedTools:      result.UsedTools,
	})
	if err != nil {
		result.Checks = append(result.Checks, CheckResult{Name: "judge_score", Pass: false, Detail: fmt.Sprintf("judge error: %v", err)})
		return result
	}
	result.Judge = &score
	pass := score.Score >= minScore
	detail := fmt.Sprintf("score %.3f < min %.3f", score.Score, minScore)
	if pass {
		detail = fmt.Sprintf("score %.3f >= min %.3f", score.Score, minScore)
	}
	if strings.TrimSpace(score.Reason) != "" {
		detail = detail + "; " + score.Reason
	}
	result.Checks = append(result.Checks, CheckResult{Name: "judge_score", Pass: pass, Detail: detail})
	return result
}

func defaultWorkers(total int) int {
	if total <= 1 {
		return 1
	}
	cpu := runtime.NumCPU()
	if cpu < 2 {
		cpu = 2
	}
	if cpu > 8 {
		cpu = 8
	}
	if cpu > total {
		cpu = total
	}
	return cpu
}

func backoffForAttempt(base time.Duration, attempt int) time.Duration {
	if base <= 0 {
		base = 400 * time.Millisecond
	}
	if attempt < 1 {
		attempt = 1
	}
	mult := 1 << (attempt - 1)
	if mult > 16 {
		mult = 16
	}
	return time.Duration(mult) * base
}

func extractUsedTools(result types.RunResult) []string {
	seen := map[string]struct{}{}
	used := make([]string, 0, 4)

	for _, ev := range result.Events {
		if ev.Type != types.EventBeforeTool {
			continue
		}
		name := strings.TrimSpace(ev.ToolName)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		used = append(used, name)
	}
	if len(used) > 0 {
		return used
	}

	for _, msg := range result.Messages {
		if msg.Role != types.RoleTool {
			continue
		}
		name := strings.TrimSpace(msg.Name)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		used = append(used, name)
	}
	return used
}

func latencyFromRun(result types.RunResult, started time.Time) int64 {
	if result.StartedAt != nil && result.CompletedAt != nil {
		d := result.CompletedAt.Sub(*result.StartedAt)
		if d >= 0 {
			return d.Milliseconds()
		}
	}
	return time.Since(started).Milliseconds()
}

func allChecksPass(checks []CheckResult) bool {
	if len(checks) == 0 {
		return true
	}
	for _, check := range checks {
		if !check.Pass {
			return false
		}
	}
	return true
}

func toolChecksPass(checks []CheckResult) bool {
	passed := true
	found := false
	for _, check := range checks {
		if strings.HasPrefix(check.Name, "required_tool:") || strings.HasPrefix(check.Name, "forbidden_tool:") {
			found = true
			if !check.Pass {
				passed = false
			}
		}
	}
	if !found {
		return true
	}
	return passed
}

func containsString(items []string, needle string) bool {
	for _, item := range items {
		if strings.EqualFold(strings.TrimSpace(item), strings.TrimSpace(needle)) {
			return true
		}
	}
	return false
}

func ratio(numerator, denominator int) float64 {
	if denominator == 0 {
		return 0
	}
	return (float64(numerator) / float64(denominator)) * 100
}

func averageInt64(values []int64) float64 {
	if len(values) == 0 {
		return 0
	}
	var sum int64
	for _, v := range values {
		sum += v
	}
	return float64(sum) / float64(len(values))
}

func percentile(values []int64, p int) int64 {
	if len(values) == 0 {
		return 0
	}
	copyVals := append([]int64(nil), values...)
	sort.Slice(copyVals, func(i, j int) bool { return copyVals[i] < copyVals[j] })
	if p <= 0 {
		return copyVals[0]
	}
	if p >= 100 {
		return copyVals[len(copyVals)-1]
	}
	idx := int((float64(p) / 100) * float64(len(copyVals)-1))
	if idx < 0 {
		idx = 0
	}
	if idx >= len(copyVals) {
		idx = len(copyVals) - 1
	}
	return copyVals[idx]
}
