package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/PipeOpsHQ/agent-sdk-go/delivery"
	"github.com/PipeOpsHQ/agent-sdk-go/devui/auth"
	"github.com/PipeOpsHQ/agent-sdk-go/flow"
)

type flowCreateRequest struct {
	Flow       flow.Definition `json:"flow"`
	Legacy     flow.Definition `json:"-"`
	Persist    bool            `json:"persist"`
	HasPersist bool            `json:"-"`
}

func (r *flowCreateRequest) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if b, ok := raw["flow"]; ok {
		if err := json.Unmarshal(b, &r.Flow); err != nil {
			return err
		}
	} else {
		if err := json.Unmarshal(data, &r.Legacy); err != nil {
			return err
		}
	}
	if b, ok := raw["persist"]; ok {
		r.HasPersist = true
		if err := json.Unmarshal(b, &r.Persist); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) handleFlowByName(w http.ResponseWriter, r *http.Request, p principal) {
	trimmed := strings.TrimPrefix(r.URL.Path, "/api/v1/flows/")
	parts := splitPath(trimmed)
	if len(parts) == 0 || parts[0] == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("flow name is required"))
		return
	}
	name := parts[0]
	if len(parts) == 2 && parts[1] == "run" {
		s.handleRunNamedFlow(w, r, p, name)
		return
	}
	if len(parts) > 1 {
		writeError(w, http.StatusNotFound, fmt.Errorf("unsupported flow endpoint"))
		return
	}

	switch r.Method {
	case "GET":
		f, ok := flow.Get(name)
		if !ok {
			writeError(w, http.StatusNotFound, fmt.Errorf("flow %q not found", name))
			return
		}
		writeJSON(w, http.StatusOK, f)
	case "DELETE":
		if p.Role.Rank() < auth.RoleOperator.Rank() {
			writeError(w, http.StatusForbidden, fmt.Errorf("insufficient role: requires %s", auth.RoleOperator))
			return
		}
		if !flow.Delete(name) {
			writeError(w, http.StatusNotFound, fmt.Errorf("flow %q not found", name))
			return
		}
		if err := s.removeFlowSpec(name); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		s.audit(r.Context(), p, "flow.delete", "flows", map[string]any{"name": name})
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "name": name})
	default:
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
	}
}

func (s *Server) handleRunNamedFlow(w http.ResponseWriter, r *http.Request, _ principal, flowName string) {
	if r.Method != "POST" {
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
		return
	}
	if s.cfg.Playground == nil {
		writeError(w, http.StatusNotImplemented, fmt.Errorf("playground runner not configured"))
		return
	}
	var body struct {
		Input   string           `json:"input"`
		ReplyTo *delivery.Target `json:"replyTo,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	input := strings.TrimSpace(body.Input)
	if input == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("input is required"))
		return
	}
	resp, err := s.cfg.Playground.Run(r.Context(), PlaygroundRequest{Input: input, Flow: flowName, ReplyTo: delivery.Normalize(body.ReplyTo)})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if strings.TrimSpace(resp.Status) == "" {
		resp.Status = "completed"
	}
	writeJSON(w, http.StatusOK, resp)
}

var flowFileNamePattern = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

func (s *Server) persistFlowSpec(def flow.Definition) error {
	dir := strings.TrimSpace(s.cfg.FlowSpecDir)
	if dir == "" {
		dir = "./.ai-agent/flows"
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create flow dir: %w", err)
	}
	name := sanitizeFlowFileName(def.Name)
	if name == "" {
		return fmt.Errorf("invalid flow name")
	}
	path := filepath.Join(dir, name+".json")
	content, err := json.MarshalIndent(def, "", "  ")
	if err != nil {
		return fmt.Errorf("encode flow: %w", err)
	}
	if err := os.WriteFile(path, content, 0644); err != nil {
		return fmt.Errorf("write flow spec: %w", err)
	}
	return nil
}

func (s *Server) removeFlowSpec(name string) error {
	dir := strings.TrimSpace(s.cfg.FlowSpecDir)
	if dir == "" {
		dir = "./.ai-agent/flows"
	}
	path := filepath.Join(dir, sanitizeFlowFileName(name)+".json")
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func sanitizeFlowFileName(name string) string {
	name = strings.TrimSpace(strings.ToLower(name))
	name = flowFileNamePattern.ReplaceAllString(name, "-")
	name = strings.Trim(name, "-.")
	if len(name) > 80 {
		name = name[:80]
	}
	return name
}
