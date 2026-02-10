package multiagent

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/PipeOpsHQ/agent-sdk-go/tools"
)

// createMemoryReadTool creates a tool for reading from shared memory.
func (o *Orchestrator) createMemoryReadTool() tools.Tool {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"key": map[string]any{
				"type":        "string",
				"description": "The key to read from shared memory.",
			},
			"list_keys": map[string]any{
				"type":        "boolean",
				"description": "If true, list all available keys instead of reading a specific key.",
			},
		},
	}

	return tools.NewFuncTool(
		"shared_memory_read",
		"Read values from shared memory that is accessible to all agents in the system.",
		schema,
		func(ctx context.Context, args json.RawMessage) (any, error) {
			var input struct {
				Key      string `json:"key"`
				ListKeys bool   `json:"list_keys"`
			}
			if err := json.Unmarshal(args, &input); err != nil {
				return nil, err
			}

			if o.memory == nil {
				return map[string]any{"error": "shared memory not enabled"}, nil
			}

			if input.ListKeys {
				return map[string]any{
					"keys": o.memory.Keys(),
				}, nil
			}

			if input.Key == "" {
				return map[string]any{"error": "key is required"}, nil
			}

			value, found := o.memory.Get(input.Key)
			if !found {
				return map[string]any{
					"found": false,
					"key":   input.Key,
				}, nil
			}

			return map[string]any{
				"found": true,
				"key":   input.Key,
				"value": value,
			}, nil
		},
	)
}

// createMemoryWriteTool creates a tool for writing to shared memory.
func (o *Orchestrator) createMemoryWriteTool() tools.Tool {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"key": map[string]any{
				"type":        "string",
				"description": "The key to store the value under.",
			},
			"value": map[string]any{
				"type":        "string",
				"description": "The value to store (can be JSON for complex data).",
			},
			"ttl_seconds": map[string]any{
				"type":        "integer",
				"description": "Optional time-to-live in seconds.",
			},
		},
		"required": []string{"key", "value"},
	}

	return tools.NewFuncTool(
		"shared_memory_write",
		"Write values to shared memory that is accessible to all agents in the system.",
		schema,
		func(ctx context.Context, args json.RawMessage) (any, error) {
			var input struct {
				Key        string `json:"key"`
				Value      string `json:"value"`
				TTLSeconds int    `json:"ttl_seconds"`
			}
			if err := json.Unmarshal(args, &input); err != nil {
				return nil, err
			}

			if o.memory == nil {
				return map[string]any{"error": "shared memory not enabled"}, nil
			}

			if input.Key == "" {
				return map[string]any{"error": "key is required"}, nil
			}

			// Try to parse value as JSON
			var value any = input.Value
			var jsonValue any
			if err := json.Unmarshal([]byte(input.Value), &jsonValue); err == nil {
				value = jsonValue
			}

			// Get agent ID from context if available
			agentID := "unknown"
			if id := ctx.Value("agentId"); id != nil {
				if s, ok := id.(string); ok {
					agentID = s
				}
			}

			if input.TTLSeconds > 0 {
				o.memory.SetWithTTL(input.Key, value, agentID, time.Duration(input.TTLSeconds)*time.Second)
			} else {
				o.memory.Set(input.Key, value, agentID)
			}

			return map[string]any{
				"success": true,
				"key":     input.Key,
			}, nil
		},
	)
}

// createDelegationTool creates a tool for delegating tasks to worker agents.
func (o *Orchestrator) createDelegationTool() tools.Tool {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"agent_id": map[string]any{
				"type":        "string",
				"description": "The ID of the worker agent to delegate to.",
			},
			"task": map[string]any{
				"type":        "string",
				"description": "The task description to send to the worker agent.",
			},
		},
		"required": []string{"agent_id", "task"},
	}

	return tools.NewFuncTool(
		"delegate_task",
		"Delegate a task to a worker agent. Use this to assign specific tasks to specialized workers.",
		schema,
		func(ctx context.Context, args json.RawMessage) (any, error) {
			var input struct {
				AgentID string `json:"agent_id"`
				Task    string `json:"task"`
			}
			if err := json.Unmarshal(args, &input); err != nil {
				return nil, err
			}

			if input.AgentID == "" {
				return map[string]any{"error": "agent_id is required"}, nil
			}
			if input.Task == "" {
				return map[string]any{"error": "task is required"}, nil
			}

			// Get delegation context
			dc := ctx.Value(delegationContextKey)
			if dc == nil {
				return map[string]any{"error": "delegation context not available"}, nil
			}

			delegCtx, ok := dc.(*delegationContext)
			if !ok {
				return map[string]any{"error": "invalid delegation context"}, nil
			}

			// Find the worker
			var worker *ManagedAgent
			for _, w := range delegCtx.workers {
				if w.ID == input.AgentID {
					worker = w
					break
				}
			}

			if worker == nil {
				return map[string]any{
					"error":   "worker not found",
					"agentId": input.AgentID,
				}, nil
			}

			// Execute the task on the worker
			output, err := worker.Agent.Run(ctx, input.Task)
			if err != nil {
				return map[string]any{
					"success": false,
					"agentId": input.AgentID,
					"agent":   worker.Name,
					"error":   err.Error(),
				}, nil
			}

			// Store result
			delegCtx.mu.Lock()
			delegCtx.results[input.AgentID] = output
			delegCtx.mu.Unlock()

			return map[string]any{
				"success": true,
				"agentId": input.AgentID,
				"agent":   worker.Name,
				"result":  output,
			}, nil
		},
	)
}

// createAgentCallTool creates a tool for calling other agents directly.
func (o *Orchestrator) createAgentCallTool() tools.Tool {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"agent_id": map[string]any{
				"type":        "string",
				"description": "The ID of the agent to call.",
			},
			"agent_name": map[string]any{
				"type":        "string",
				"description": "The name of the agent to call (alternative to agent_id).",
			},
			"message": map[string]any{
				"type":        "string",
				"description": "The message/prompt to send to the agent.",
			},
		},
		"required": []string{"message"},
	}

	return tools.NewFuncTool(
		"call_agent",
		"Call another agent in the system with a message and get their response.",
		schema,
		func(ctx context.Context, args json.RawMessage) (any, error) {
			var input struct {
				AgentID   string `json:"agent_id"`
				AgentName string `json:"agent_name"`
				Message   string `json:"message"`
			}
			if err := json.Unmarshal(args, &input); err != nil {
				return nil, err
			}

			if input.Message == "" {
				return map[string]any{"error": "message is required"}, nil
			}

			// Find agent
			var target *ManagedAgent
			o.mu.RLock()
			for _, ag := range o.agents {
				if (input.AgentID != "" && ag.ID == input.AgentID) ||
					(input.AgentName != "" && ag.Name == input.AgentName) {
					target = ag
					break
				}
			}
			o.mu.RUnlock()

			if target == nil {
				return map[string]any{
					"error":     "agent not found",
					"agentId":   input.AgentID,
					"agentName": input.AgentName,
				}, nil
			}

			// Call the agent
			agentCtx, cancel := context.WithTimeout(ctx, o.config.TimeoutPerAgent)
			defer cancel()

			output, err := target.Agent.Run(agentCtx, input.Message)
			if err != nil {
				return map[string]any{
					"success": false,
					"agentId": target.ID,
					"agent":   target.Name,
					"error":   err.Error(),
				}, nil
			}

			return map[string]any{
				"success":  true,
				"agentId":  target.ID,
				"agent":    target.Name,
				"response": output,
			}, nil
		},
	)
}

// createListAgentsTool creates a tool for listing available agents.
func (o *Orchestrator) createListAgentsTool() tools.Tool {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"role": map[string]any{
				"type":        "string",
				"description": "Optional filter by role (worker, supervisor, router, specialist).",
			},
		},
	}

	return tools.NewFuncTool(
		"list_agents",
		"List all available agents in the multi-agent system.",
		schema,
		func(ctx context.Context, args json.RawMessage) (any, error) {
			var input struct {
				Role string `json:"role"`
			}
			_ = json.Unmarshal(args, &input)

			agents := o.registry.List()

			if input.Role != "" {
				filtered := make([]AgentInfo, 0)
				for _, ag := range agents {
					if string(ag.Role) == input.Role {
						filtered = append(filtered, ag)
					}
				}
				agents = filtered
			}

			result := make([]map[string]any, 0, len(agents))
			for _, ag := range agents {
				result = append(result, map[string]any{
					"id":           ag.ID,
					"name":         ag.Name,
					"description":  ag.Description,
					"role":         ag.Role,
					"capabilities": ag.Capabilities,
					"status":       ag.Status,
				})
			}

			return map[string]any{
				"agents": result,
				"count":  len(result),
			}, nil
		},
	)
}

// AddAgentTools adds multi-agent collaboration tools to all agents.
func (o *Orchestrator) AddAgentTools() {
	callTool := o.createAgentCallTool()
	listTool := o.createListAgentsTool()

	o.mu.Lock()
	defer o.mu.Unlock()

	for _, ag := range o.agents {
		ag.Agent.RegisterTool(callTool)
		ag.Agent.RegisterTool(listTool)
	}
}

// SendMessage sends a message from one agent to another.
func (o *Orchestrator) SendMessage(ctx context.Context, from, to, message string) (string, error) {
	toAgent, ok := o.GetAgent(to)
	if !ok {
		return "", fmt.Errorf("target agent %q not found", to)
	}

	fromAgent, _ := o.GetAgent(from)
	fromName := from
	if fromAgent != nil {
		fromName = fromAgent.Name
	}

	prefixedMessage := fmt.Sprintf("[Message from %s]: %s", fromName, message)

	agentCtx, cancel := context.WithTimeout(ctx, o.config.TimeoutPerAgent)
	defer cancel()

	return toAgent.Agent.Run(agentCtx, prefixedMessage)
}
