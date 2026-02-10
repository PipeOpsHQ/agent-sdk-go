package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/PipeOpsHQ/agent-sdk-go/devui/auth"
	fwtools "github.com/PipeOpsHQ/agent-sdk-go/tools"
)

type customToolUpsertRequest struct {
	Tool       fwtools.CustomHTTPSpec `json:"tool"`
	Persist    bool                   `json:"persist"`
	HasPersist bool
	Legacy     fwtools.CustomHTTPSpec
}

func (r *customToolUpsertRequest) UnmarshalJSON(data []byte) error {
	type alias customToolUpsertRequest
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	var a alias
	if v, ok := raw["tool"]; ok {
		if err := json.Unmarshal(v, &a.Tool); err != nil {
			return err
		}
	} else {
		if err := json.Unmarshal(data, &a.Legacy); err != nil {
			return err
		}
	}
	if v, ok := raw["persist"]; ok {
		a.HasPersist = true
		if err := json.Unmarshal(v, &a.Persist); err != nil {
			return err
		}
	}
	*r = customToolUpsertRequest(a)
	return nil
}

func (s *Server) handleCustomTools(w http.ResponseWriter, r *http.Request, p principal) {
	switch r.Method {
	case http.MethodGet:
		specs := fwtools.ListCustomHTTPTools()
		writeJSON(w, http.StatusOK, map[string]any{"tools": specs, "count": len(specs)})
	case http.MethodPost:
		if p.Role.Rank() < auth.RoleOperator.Rank() {
			writeError(w, http.StatusForbidden, fmt.Errorf("insufficient role: requires %s", auth.RoleOperator))
			return
		}
		var req customToolUpsertRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		spec := req.Tool
		if strings.TrimSpace(spec.Name) == "" {
			spec = req.Legacy
		}
		if err := fwtools.UpsertCustomHTTPTool(spec); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		persist := req.Persist
		if !req.HasPersist {
			persist = true
		}
		if persist {
			if err := s.persistCustomToolSpec(spec); err != nil {
				writeError(w, http.StatusInternalServerError, err)
				return
			}
		}
		s.audit(r.Context(), p, "tool.custom.upsert", "tools", map[string]any{"name": spec.Name})
		writeJSON(w, http.StatusCreated, map[string]any{"ok": true, "tool": spec, "persisted": persist})
	default:
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
	}
}

func (s *Server) handleCustomToolByName(w http.ResponseWriter, r *http.Request, p principal) {
	name := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/api/v1/tools/custom/"))
	if name == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("tool name is required"))
		return
	}

	if strings.Contains(name, "/") {
		writeError(w, http.StatusNotFound, fmt.Errorf("unsupported tools endpoint"))
		return
	}

	switch r.Method {
	case http.MethodGet:
		for _, spec := range fwtools.ListCustomHTTPTools() {
			if spec.Name == name {
				writeJSON(w, http.StatusOK, spec)
				return
			}
		}
		writeError(w, http.StatusNotFound, fmt.Errorf("custom tool %q not found", name))
	case http.MethodDelete:
		if p.Role.Rank() < auth.RoleOperator.Rank() {
			writeError(w, http.StatusForbidden, fmt.Errorf("insufficient role: requires %s", auth.RoleOperator))
			return
		}
		if !fwtools.DeleteCustomHTTPTool(name) {
			writeError(w, http.StatusNotFound, fmt.Errorf("custom tool %q not found", name))
			return
		}
		if err := s.removeCustomToolSpec(name); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		s.audit(r.Context(), p, "tool.custom.delete", "tools", map[string]any{"name": name})
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "name": name})
	default:
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
	}
}

func (s *Server) persistCustomToolSpec(spec fwtools.CustomHTTPSpec) error {
	dir := strings.TrimSpace(s.cfg.ToolSpecDir)
	if dir == "" {
		dir = "./.ai-agent/tools"
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create tools directory: %w", err)
	}
	content, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(dir, sanitizeCustomToolFileName(spec.Name)+".json")
	if err := os.WriteFile(path, content, 0644); err != nil {
		return fmt.Errorf("write tool spec: %w", err)
	}
	return nil
}

func (s *Server) removeCustomToolSpec(name string) error {
	dir := strings.TrimSpace(s.cfg.ToolSpecDir)
	if dir == "" {
		dir = "./.ai-agent/tools"
	}
	path := filepath.Join(dir, sanitizeCustomToolFileName(name)+".json")
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove tool spec: %w", err)
	}
	return nil
}

var customToolFileNameSanitizer = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

func sanitizeCustomToolFileName(name string) string {
	v := strings.TrimSpace(name)
	v = strings.ReplaceAll(v, " ", "-")
	v = customToolFileNameSanitizer.ReplaceAllString(v, "-")
	v = strings.Trim(v, "-._")
	if v == "" {
		return "custom-tool"
	}
	if len(v) > 120 {
		v = v[:120]
	}
	return v
}
