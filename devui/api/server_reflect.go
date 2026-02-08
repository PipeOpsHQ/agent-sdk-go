package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/PipeOpsHQ/agent-sdk-go/framework/flow"
	fwtools "github.com/PipeOpsHQ/agent-sdk-go/framework/tools"
	"github.com/PipeOpsHQ/agent-sdk-go/framework/workflow"
)

// Action represents a single discoverable action in the registry.
// Mirrors Genkit's unified action concept.
type Action struct {
	Key          string         `json:"key"`
	Name         string         `json:"name"`
	Type         string         `json:"type"`
	Description  string         `json:"description,omitempty"`
	InputSchema  map[string]any `json:"inputSchema,omitempty"`
	OutputSchema map[string]any `json:"outputSchema,omitempty"`
	Metadata     map[string]any `json:"metadata,omitempty"`
}

// handleReflect returns all registered actions (flows, tools, workflows) in one response.
func (s *Server) handleReflect(w http.ResponseWriter, r *http.Request, _ principal) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
		return
	}

	var actions []Action

	// Flows
	for _, f := range flow.All() {
		a := Action{
			Key:          "/flow/" + f.Name,
			Name:         f.Name,
			Type:         "flow",
			Description:  f.Description,
			InputSchema:  f.InputSchema,
			OutputSchema: f.OutputSchema,
			Metadata: map[string]any{
				"workflow":     f.Workflow,
				"tools":        f.Tools,
				"inputExample": f.InputExample,
			},
		}
		actions = append(actions, a)
	}

	// Tools
	schemas := fwtools.ToolSchemas()
	for _, ti := range fwtools.ToolCatalog() {
		a := Action{
			Key:         "/tool/" + ti.Name,
			Name:        ti.Name,
			Type:        "tool",
			Description: ti.Description,
			InputSchema: schemas[ti.Name],
		}
		actions = append(actions, a)
	}

	// Workflows
	for _, name := range workflow.Names() {
		a := Action{
			Key:  "/workflow/" + name,
			Name: name,
			Type: "workflow",
		}
		actions = append(actions, a)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"actions": actions,
		"count":   len(actions),
	})
}

// runActionRequest is the request body for /api/v1/actions/run.
type runActionRequest struct {
	Key   string          `json:"key"`
	Input json.RawMessage `json:"input"`
}

// handleRunAction executes a single action (tool or flow) by key.
func (s *Server) handleRunAction(w http.ResponseWriter, r *http.Request, _ principal) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
		return
	}

	var req runActionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("invalid request body: %w", err))
		return
	}
	if req.Key == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("action key is required"))
		return
	}

	start := time.Now()

	// Parse action type and name from key (e.g. "/tool/file_system" → type="tool", name="file_system")
	actionType, actionName := parseActionKey(req.Key)

	switch actionType {
	case "tool":
		result, err := fwtools.ExecuteTool(r.Context(), actionName, req.Input)
		elapsed := time.Since(start)
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]any{
				"key":      req.Key,
				"status":   "error",
				"error":    err.Error(),
				"duration": elapsed.Milliseconds(),
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"key":      req.Key,
			"status":   "success",
			"output":   result,
			"duration": elapsed.Milliseconds(),
		})

	case "flow":
		if s.cfg.Playground == nil {
			writeError(w, http.StatusServiceUnavailable, fmt.Errorf("playground runner not configured"))
			return
		}
		// Extract input text from JSON
		var inputText string
		if req.Input != nil {
			var inputObj map[string]any
			if err := json.Unmarshal(req.Input, &inputObj); err != nil {
				// Treat as plain string
				_ = json.Unmarshal(req.Input, &inputText)
			} else if v, ok := inputObj["input"]; ok {
				inputText = fmt.Sprintf("%v", v)
			}
		}
		resp, err := s.cfg.Playground.Run(context.Background(), PlaygroundRequest{
			Input: inputText,
			Flow:  actionName,
		})
		elapsed := time.Since(start)
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]any{
				"key":      req.Key,
				"status":   "error",
				"error":    err.Error(),
				"duration": elapsed.Milliseconds(),
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"key":       req.Key,
			"status":    resp.Status,
			"output":    resp.Output,
			"runId":     resp.RunID,
			"sessionId": resp.SessionID,
			"provider":  resp.Provider,
			"duration":  elapsed.Milliseconds(),
		})

	default:
		writeError(w, http.StatusBadRequest, fmt.Errorf("unsupported action type %q (supported: tool, flow)", actionType))
	}
}

// parseActionKey splits "/type/name" into (type, name).
func parseActionKey(key string) (string, string) {
	// Strip leading slash
	if len(key) > 0 && key[0] == '/' {
		key = key[1:]
	}
	for i := 0; i < len(key); i++ {
		if key[i] == '/' {
			return key[:i], key[i+1:]
		}
	}
	return key, ""
}
