package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/PipeOpsHQ/agent-sdk-go/framework/devui/auth"
	"github.com/PipeOpsHQ/agent-sdk-go/framework/prompt"
)

func (s *Server) handlePrompts(w http.ResponseWriter, r *http.Request, p principal) {
	switch r.Method {
	case http.MethodGet:
		specs := prompt.List()
		items := make([]map[string]any, 0, len(specs))
		for _, spec := range specs {
			items = append(items, map[string]any{
				"name":        spec.Name,
				"version":     spec.Version,
				"description": spec.Description,
				"tags":        spec.Tags,
				"ref":         spec.Name + "@" + spec.Version,
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{"count": len(items), "prompts": items})
	case http.MethodPost:
		if p.Role.Rank() < auth.RoleOperator.Rank() {
			writeError(w, http.StatusForbidden, fmt.Errorf("insufficient role: requires %s", auth.RoleOperator))
			return
		}
		var req promptValidateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		normalized, err := prompt.NormalizeSpec(req.Spec)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if err := prompt.Register(normalized); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if err := s.writePromptSpecFile(normalized); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		s.audit(r.Context(), p, "prompt.upsert", "prompts", map[string]any{"ref": normalized.Name + "@" + normalized.Version})
		writeJSON(w, http.StatusCreated, map[string]any{"ok": true, "spec": normalized})
	default:
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
	}
}

func (s *Server) handlePromptByRef(w http.ResponseWriter, r *http.Request, p principal) {
	ref := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/api/v1/prompts/"))
	if ref == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("prompt ref is required"))
		return
	}
	switch r.Method {
	case http.MethodGet:
		spec, ok := prompt.Resolve(ref)
		if !ok {
			writeError(w, http.StatusNotFound, fmt.Errorf("prompt %q not found", ref))
			return
		}
		writeJSON(w, http.StatusOK, spec)
	case http.MethodDelete:
		if p.Role.Rank() < auth.RoleOperator.Rank() {
			writeError(w, http.StatusForbidden, fmt.Errorf("insufficient role: requires %s", auth.RoleOperator))
			return
		}
		if !prompt.Delete(ref) {
			writeError(w, http.StatusNotFound, fmt.Errorf("prompt %q not found", ref))
			return
		}
		if err := s.removePromptSpecFile(ref); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		s.audit(r.Context(), p, "prompt.delete", "prompts", map[string]any{"ref": ref})
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "ref": ref})
	default:
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
	}
}

type promptRenderRequest struct {
	Ref   string         `json:"ref"`
	Input map[string]any `json:"input"`
}

func (s *Server) handlePromptRender(w http.ResponseWriter, r *http.Request, _ principal) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
		return
	}
	var req promptRenderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	spec, ok := prompt.Resolve(req.Ref)
	if !ok {
		writeError(w, http.StatusNotFound, fmt.Errorf("prompt %q not found", req.Ref))
		return
	}
	rendered, err := prompt.Render(spec.System, promptInputToStrings(req.Input))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ref": req.Ref, "rendered": rendered})
}

type promptValidateRequest struct {
	Spec prompt.Spec `json:"spec"`
}

func (s *Server) handlePromptValidate(w http.ResponseWriter, r *http.Request, _ principal) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
		return
	}
	var req promptValidateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	normalized, err := prompt.NormalizeSpec(req.Spec)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"valid": true, "spec": normalized})
}

func promptInputToStrings(input map[string]any) map[string]string {
	out := map[string]string{}
	for k, v := range input {
		key := strings.TrimSpace(k)
		if key == "" {
			continue
		}
		out[key] = strings.TrimSpace(fmt.Sprintf("%v", v))
	}
	return out
}

var promptFileSanitizer = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

func (s *Server) writePromptSpecFile(spec prompt.Spec) error {
	dir := strings.TrimSpace(s.cfg.PromptSpecDir)
	if dir == "" {
		dir = "./.ai-agent/prompts"
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create prompt dir: %w", err)
	}
	fileName := sanitizePromptFileName(spec.Name + "@" + spec.Version)
	if fileName == "" {
		return fmt.Errorf("invalid prompt file name")
	}
	path := filepath.Join(dir, fileName+".json")
	content, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		return fmt.Errorf("encode prompt spec: %w", err)
	}
	if err := os.WriteFile(path, content, 0644); err != nil {
		return fmt.Errorf("write prompt spec: %w", err)
	}
	return nil
}

func (s *Server) removePromptSpecFile(ref string) error {
	dir := strings.TrimSpace(s.cfg.PromptSpecDir)
	if dir == "" {
		dir = "./.ai-agent/prompts"
	}
	path := filepath.Join(dir, sanitizePromptFileName(ref)+".json")
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove prompt spec: %w", err)
	}
	return nil
}

func sanitizePromptFileName(v string) string {
	v = strings.TrimSpace(strings.ToLower(v))
	v = promptFileSanitizer.ReplaceAllString(v, "-")
	v = strings.Trim(v, "-.")
	if len(v) > 120 {
		v = v[:120]
	}
	return v
}
