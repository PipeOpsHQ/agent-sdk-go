package multiagent

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/PipeOpsHQ/agent-sdk-go/observe"
)

// runSequential executes agents one after another, passing output as input.
func (o *Orchestrator) runSequential(ctx context.Context, runID, input string) (*MultiAgentResult, error) {
	agents := o.getOrderedAgents()
	if len(agents) == 0 {
		return nil, fmt.Errorf("no agents available for sequential execution")
	}

	result := &MultiAgentResult{
		AgentResults: make(map[string]AgentRunResult),
	}

	currentInput := input
	for i, ag := range agents {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		o.emit(ctx, observe.Event{
			Kind:   observe.KindCustom,
			Status: observe.StatusStarted,
			Name:   "agent.run",
			Attributes: map[string]any{
				"runId":    runID,
				"agentId":  ag.ID,
				"agent":    ag.Name,
				"sequence": i + 1,
			},
		})

		agentCtx, cancel := context.WithTimeout(ctx, o.config.TimeoutPerAgent)
		startTime := time.Now()

		output, err := ag.Agent.Run(agentCtx, currentInput)
		cancel()

		duration := time.Since(startTime)
		agentResult := AgentRunResult{
			AgentID:   ag.ID,
			AgentName: ag.Name,
			Duration:  duration,
		}

		if err != nil {
			agentResult.Error = err.Error()
			result.AgentResults[ag.ID] = agentResult
			o.emit(ctx, observe.Event{
				Kind:   observe.KindCustom,
				Status: observe.StatusFailed,
				Name:   "agent.run",
				Attributes: map[string]any{
					"runId":   runID,
					"agentId": ag.ID,
					"error":   err.Error(),
				},
			})
			// Continue to next agent or fail?
			// For sequential, we fail on first error
			return nil, fmt.Errorf("agent %q failed: %w", ag.Name, err)
		}

		agentResult.Output = output
		result.AgentResults[ag.ID] = agentResult
		result.FinalOutput = output
		currentInput = output // Pass output as next input

		o.emit(ctx, observe.Event{
			Kind:   observe.KindCustom,
			Status: observe.StatusCompleted,
			Name:   "agent.run",
			Attributes: map[string]any{
				"runId":    runID,
				"agentId":  ag.ID,
				"duration": duration.String(),
			},
		})
	}

	return result, nil
}

// runParallel executes all agents concurrently and aggregates results.
func (o *Orchestrator) runParallel(ctx context.Context, runID, input string) (*MultiAgentResult, error) {
	agents := o.getOrderedAgents()
	if len(agents) == 0 {
		return nil, fmt.Errorf("no agents available for parallel execution")
	}

	result := &MultiAgentResult{
		AgentResults: make(map[string]AgentRunResult),
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	results := make([]AgentRunResult, 0, len(agents))

	for _, ag := range agents {
		wg.Add(1)
		go func(ag *ManagedAgent) {
			defer wg.Done()

			o.emit(ctx, observe.Event{
				Kind:   observe.KindCustom,
				Status: observe.StatusStarted,
				Name:   "agent.run.parallel",
				Attributes: map[string]any{
					"runId":   runID,
					"agentId": ag.ID,
					"agent":   ag.Name,
				},
			})

			agentCtx, cancel := context.WithTimeout(ctx, o.config.TimeoutPerAgent)
			defer cancel()

			startTime := time.Now()
			output, err := ag.Agent.Run(agentCtx, input)
			duration := time.Since(startTime)

			agentResult := AgentRunResult{
				AgentID:   ag.ID,
				AgentName: ag.Name,
				Output:    output,
				Duration:  duration,
			}
			if err != nil {
				agentResult.Error = err.Error()
			}

			mu.Lock()
			results = append(results, agentResult)
			result.AgentResults[ag.ID] = agentResult
			mu.Unlock()

			status := observe.StatusCompleted
			if err != nil {
				status = observe.StatusFailed
			}
			o.emit(ctx, observe.Event{
				Kind:   observe.KindCustom,
				Status: status,
				Name:   "agent.run.parallel",
				Attributes: map[string]any{
					"runId":    runID,
					"agentId":  ag.ID,
					"duration": duration.String(),
				},
			})
		}(ag)
	}

	wg.Wait()

	// Aggregate results
	var outputs []string
	for _, r := range results {
		if r.Error == "" && r.Output != "" {
			outputs = append(outputs, fmt.Sprintf("[%s]: %s", r.AgentName, r.Output))
		}
	}
	result.FinalOutput = strings.Join(outputs, "\n\n")

	return result, nil
}

// runSupervisor has a supervisor agent delegate to worker agents.
func (o *Orchestrator) runSupervisor(ctx context.Context, runID, input string) (*MultiAgentResult, error) {
	// Find supervisor
	var supervisor *ManagedAgent
	var workers []*ManagedAgent

	o.mu.RLock()
	for _, ag := range o.agents {
		if ag.Role == RoleSupervisor {
			supervisor = ag
		} else {
			workers = append(workers, ag)
		}
	}
	o.mu.RUnlock()

	if supervisor == nil {
		return nil, fmt.Errorf("no supervisor agent found")
	}
	if len(workers) == 0 {
		return nil, fmt.Errorf("no worker agents found")
	}

	result := &MultiAgentResult{
		AgentResults: make(map[string]AgentRunResult),
	}

	// Build context for supervisor about available workers
	workersInfo := make([]string, 0, len(workers))
	for _, w := range workers {
		workersInfo = append(workersInfo, fmt.Sprintf("- %s (ID: %s): %s", w.Name, w.ID, w.Description))
	}

	supervisorInput := fmt.Sprintf(`You are a supervisor agent managing a team of worker agents.

Available workers:
%s

User request: %s

Analyze the request and delegate tasks to appropriate workers using the delegate_task tool.
After receiving all worker responses, synthesize a final answer.`, strings.Join(workersInfo, "\n"), input)

	// Store workers in context for delegation tool
	delegationCtx := context.WithValue(ctx, delegationContextKey, &delegationContext{
		runID:   runID,
		workers: workers,
		results: make(map[string]string),
	})

	o.emit(ctx, observe.Event{
		Kind:   observe.KindCustom,
		Status: observe.StatusStarted,
		Name:   "supervisor.run",
		Attributes: map[string]any{
			"runId":       runID,
			"supervisor":  supervisor.Name,
			"workerCount": len(workers),
		},
	})

	startTime := time.Now()
	output, err := supervisor.Agent.Run(delegationCtx, supervisorInput)
	duration := time.Since(startTime)

	result.AgentResults[supervisor.ID] = AgentRunResult{
		AgentID:   supervisor.ID,
		AgentName: supervisor.Name,
		Output:    output,
		Duration:  duration,
	}

	if err != nil {
		result.AgentResults[supervisor.ID] = AgentRunResult{
			AgentID:   supervisor.ID,
			AgentName: supervisor.Name,
			Error:     err.Error(),
			Duration:  duration,
		}
		return nil, fmt.Errorf("supervisor failed: %w", err)
	}

	result.FinalOutput = output

	// Add worker results from delegation context
	if dc := delegationCtx.Value(delegationContextKey); dc != nil {
		if delegCtx, ok := dc.(*delegationContext); ok {
			for workerID, workerOutput := range delegCtx.results {
				if ag, found := o.agents[workerID]; found {
					result.AgentResults[workerID] = AgentRunResult{
						AgentID:   workerID,
						AgentName: ag.Name,
						Output:    workerOutput,
					}
				}
			}
		}
	}

	return result, nil
}

// runRouter routes input to the most appropriate agent.
func (o *Orchestrator) runRouter(ctx context.Context, runID, input string) (*MultiAgentResult, error) {
	// Find router agent
	var router *ManagedAgent
	var specialists []*ManagedAgent

	o.mu.RLock()
	for _, ag := range o.agents {
		if ag.Role == RoleRouter {
			router = ag
		} else {
			specialists = append(specialists, ag)
		}
	}
	o.mu.RUnlock()

	if router == nil {
		return nil, fmt.Errorf("no router agent found")
	}
	if len(specialists) == 0 {
		return nil, fmt.Errorf("no specialist agents found")
	}

	result := &MultiAgentResult{
		AgentResults: make(map[string]AgentRunResult),
	}

	// Build routing prompt
	specialistsInfo := make([]string, 0, len(specialists))
	for _, s := range specialists {
		specialistsInfo = append(specialistsInfo, fmt.Sprintf("- %s (ID: %s): %s", s.Name, s.ID, s.Description))
	}

	routerInput := fmt.Sprintf(`You are a router agent. Analyze the user's request and select the most appropriate specialist agent.

Available specialists:
%s

User request: %s

Respond with ONLY the ID of the specialist agent that should handle this request.`, strings.Join(specialistsInfo, "\n"), input)

	// Get router's decision
	startTime := time.Now()
	routerOutput, err := router.Agent.Run(ctx, routerInput)
	if err != nil {
		return nil, fmt.Errorf("router failed: %w", err)
	}

	result.AgentResults[router.ID] = AgentRunResult{
		AgentID:   router.ID,
		AgentName: router.Name,
		Output:    routerOutput,
		Duration:  time.Since(startTime),
	}

	// Find selected specialist
	selectedID := strings.TrimSpace(routerOutput)
	var selected *ManagedAgent
	for _, s := range specialists {
		if s.ID == selectedID || strings.Contains(strings.ToLower(routerOutput), strings.ToLower(s.ID)) {
			selected = s
			break
		}
		// Also check by name
		if strings.Contains(strings.ToLower(routerOutput), strings.ToLower(s.Name)) {
			selected = s
			break
		}
	}

	if selected == nil {
		// Default to first specialist
		selected = specialists[0]
	}

	result.SelectedAgent = selected.ID

	// Run selected specialist
	o.emit(ctx, observe.Event{
		Kind:   observe.KindCustom,
		Status: observe.StatusStarted,
		Name:   "specialist.run",
		Attributes: map[string]any{
			"runId":      runID,
			"specialist": selected.Name,
		},
	})

	startTime = time.Now()
	output, err := selected.Agent.Run(ctx, input)
	duration := time.Since(startTime)

	result.AgentResults[selected.ID] = AgentRunResult{
		AgentID:   selected.ID,
		AgentName: selected.Name,
		Output:    output,
		Duration:  duration,
	}

	if err != nil {
		result.AgentResults[selected.ID] = AgentRunResult{
			AgentID:   selected.ID,
			AgentName: selected.Name,
			Error:     err.Error(),
			Duration:  duration,
		}
		return nil, fmt.Errorf("specialist %q failed: %w", selected.Name, err)
	}

	result.FinalOutput = output
	return result, nil
}

// runDebate has agents argue different perspectives.
func (o *Orchestrator) runDebate(ctx context.Context, runID, input string) (*MultiAgentResult, error) {
	agents := o.getOrderedAgents()
	if len(agents) < 2 {
		return nil, fmt.Errorf("debate requires at least 2 agents")
	}

	result := &MultiAgentResult{
		AgentResults: make(map[string]AgentRunResult),
	}

	debateHistory := []string{fmt.Sprintf("Topic: %s", input)}

	for round := 1; round <= o.config.MaxRounds; round++ {
		for _, ag := range agents {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			default:
			}

			debatePrompt := fmt.Sprintf(`You are participating in a debate. Here is the debate history so far:

%s

It is now your turn to present your perspective. Consider the arguments made by others and respond with your viewpoint.
Be concise but thorough.`, strings.Join(debateHistory, "\n\n"))

			startTime := time.Now()
			output, err := ag.Agent.Run(ctx, debatePrompt)
			duration := time.Since(startTime)

			if err != nil {
				continue // Skip failed agents in debate
			}

			debateHistory = append(debateHistory, fmt.Sprintf("[%s - Round %d]: %s", ag.Name, round, output))

			// Update result
			existing := result.AgentResults[ag.ID]
			existing.AgentID = ag.ID
			existing.AgentName = ag.Name
			existing.Output = output // Last output
			existing.Duration += duration
			result.AgentResults[ag.ID] = existing
		}
		result.Rounds = round
	}

	// Final summary
	result.FinalOutput = strings.Join(debateHistory, "\n\n")
	return result, nil
}

// runConsensus requires agents to reach agreement.
func (o *Orchestrator) runConsensus(ctx context.Context, runID, input string) (*MultiAgentResult, error) {
	agents := o.getOrderedAgents()
	if len(agents) < 2 {
		return nil, fmt.Errorf("consensus requires at least 2 agents")
	}

	result := &MultiAgentResult{
		AgentResults: make(map[string]AgentRunResult),
	}

	for round := 1; round <= o.config.MaxRounds; round++ {
		// Gather all responses
		responses := make(map[string]string)

		for _, ag := range agents {
			var consensusPrompt string
			if round == 1 {
				consensusPrompt = fmt.Sprintf(`Question: %s

Provide your answer. In subsequent rounds, you'll see other agents' responses and can revise your answer.`, input)
			} else {
				// Include previous responses
				prevResponses := make([]string, 0)
				for name, resp := range responses {
					prevResponses = append(prevResponses, fmt.Sprintf("[%s]: %s", name, resp))
				}
				consensusPrompt = fmt.Sprintf(`Question: %s

Previous responses from other agents:
%s

Review the responses and provide your answer. If you agree with a response, you can adopt it. 
If there's consensus emerging, indicate that.`, input, strings.Join(prevResponses, "\n\n"))
			}

			startTime := time.Now()
			output, err := ag.Agent.Run(ctx, consensusPrompt)
			duration := time.Since(startTime)

			if err != nil {
				continue
			}

			responses[ag.Name] = output
			existing := result.AgentResults[ag.ID]
			existing.AgentID = ag.ID
			existing.AgentName = ag.Name
			existing.Output = output
			existing.Duration += duration
			result.AgentResults[ag.ID] = existing
		}

		// Check for consensus (simple heuristic: if responses are similar)
		if checkConsensus(responses) {
			result.ConsensusReach = true
			result.Rounds = round
			// Use any response as final
			for _, resp := range responses {
				result.FinalOutput = resp
				break
			}
			return result, nil
		}

		result.Rounds = round
	}

	// No consensus reached - use voting or first response
	result.ConsensusReach = false
	for _, r := range result.AgentResults {
		if r.Output != "" {
			result.FinalOutput = r.Output
			break
		}
	}

	return result, nil
}

func (o *Orchestrator) getOrderedAgents() []*ManagedAgent {
	o.mu.RLock()
	defer o.mu.RUnlock()

	agents := make([]*ManagedAgent, 0, len(o.agents))
	for _, ag := range o.agents {
		agents = append(agents, ag)
	}
	return agents
}

// checkConsensus is a simple heuristic to check if responses are similar.
func checkConsensus(responses map[string]string) bool {
	if len(responses) < 2 {
		return false
	}

	// Simple heuristic: check if responses contain similar keywords
	// In practice, you'd want something more sophisticated
	values := make([]string, 0, len(responses))
	for _, v := range responses {
		values = append(values, strings.ToLower(v))
	}

	// Check if first 100 chars are similar
	if len(values) >= 2 {
		first := values[0]
		if len(first) > 100 {
			first = first[:100]
		}
		for _, v := range values[1:] {
			check := v
			if len(check) > 100 {
				check = check[:100]
			}
			// Very simple similarity - in production use proper NLP
			if !strings.Contains(v, first[:min(20, len(first))]) {
				return false
			}
		}
	}

	return true
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// Delegation context for supervisor pattern
type contextKey string

const delegationContextKey contextKey = "delegation"

type delegationContext struct {
	runID   string
	workers []*ManagedAgent
	results map[string]string
	mu      sync.Mutex
}
