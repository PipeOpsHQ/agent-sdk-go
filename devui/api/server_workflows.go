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
	"github.com/PipeOpsHQ/agent-sdk-go/framework/devui/catalog"
	"github.com/PipeOpsHQ/agent-sdk-go/framework/workflow"
)

func (s *Server) handleWorkflows(w http.ResponseWriter, r *http.Request, _ principal) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
		return
	}
	if s.cfg.CatalogStore == nil {
		writeJSON(w, http.StatusOK, []catalog.WorkflowToolBinding{})
		return
	}
	items, err := s.cfg.CatalogStore.ListWorkflowBindings(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) handleWorkflowRegistry(w http.ResponseWriter, r *http.Request, p principal) {
	switch r.Method {
	case http.MethodGet:
		names := workflow.Names()
		items := make([]map[string]any, 0, len(names))
		for _, name := range names {
			items = append(items, map[string]any{
				"name": name,
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"workflows": items,
			"count":     len(items),
		})
	case http.MethodPost:
		if p.Role.Rank() < auth.RoleOperator.Rank() {
			writeError(w, http.StatusForbidden, fmt.Errorf("insufficient role: requires %s", auth.RoleOperator))
			return
		}
		var req workflowRegistryCreateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		spec := req.Spec
		if strings.TrimSpace(req.Name) != "" {
			spec.Name = strings.TrimSpace(req.Name)
		}
		if strings.TrimSpace(req.Description) != "" {
			spec.Description = strings.TrimSpace(req.Description)
		}
		builder, err := workflow.NewFileBuilder(spec)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if err := workflow.Register(builder); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if req.Persist {
			if err := s.persistWorkflowSpec(builder.Name(), spec); err != nil {
				writeError(w, http.StatusInternalServerError, err)
				return
			}
		}
		writeJSON(w, http.StatusCreated, map[string]any{
			"name":        builder.Name(),
			"description": builder.Description(),
			"persisted":   req.Persist,
		})
	default:
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
	}
}

type workflowRegistryCreateRequest struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Spec        workflow.FileSpec `json:"spec"`
	Persist     bool              `json:"persist"`
}

var workflowFileNamePattern = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

func (s *Server) persistWorkflowSpec(name string, spec workflow.FileSpec) error {
	dir := strings.TrimSpace(s.cfg.WorkflowSpecDir)
	if dir == "" {
		dir = "./.ai-agent/workflows"
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create workflow dir: %w", err)
	}
	fileName := sanitizeWorkflowFileName(name)
	if fileName == "" {
		return fmt.Errorf("workflow name is required")
	}
	path := filepath.Join(dir, fileName+".json")
	content, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		return fmt.Errorf("encode workflow spec: %w", err)
	}
	if err := os.WriteFile(path, content, 0644); err != nil {
		return fmt.Errorf("write workflow spec: %w", err)
	}
	return nil
}

func sanitizeWorkflowFileName(name string) string {
	name = strings.TrimSpace(strings.ToLower(name))
	name = workflowFileNamePattern.ReplaceAllString(name, "-")
	name = strings.Trim(name, "-.")
	if name == "" {
		return ""
	}
	if len(name) > 80 {
		return name[:80]
	}
	return name
}

func (s *Server) handleWorkflowBindingByID(w http.ResponseWriter, r *http.Request, p principal) {
	if s.cfg.CatalogStore == nil {
		writeError(w, http.StatusNotImplemented, fmt.Errorf("catalog store not configured"))
		return
	}
	parts := splitPath(strings.TrimPrefix(r.URL.Path, "/api/v1/workflows/"))
	if len(parts) != 2 {
		writeError(w, http.StatusNotFound, fmt.Errorf("unsupported workflow endpoint"))
		return
	}
	workflowName := parts[0]
	if workflowName == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("workflow is required"))
		return
	}
	if parts[1] == "topology" {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
			return
		}
		topology := s.workflowTopology(r.Context(), workflowName)
		writeJSON(w, http.StatusOK, topology)
		return
	}
	if parts[1] != "tool-bindings" {
		writeError(w, http.StatusNotFound, fmt.Errorf("unsupported workflow endpoint"))
		return
	}
	switch r.Method {
	case http.MethodPatch:
		if p.Role.Rank() < auth.RoleOperator.Rank() {
			writeError(w, http.StatusForbidden, fmt.Errorf("insufficient role: requires %s", auth.RoleOperator))
			return
		}
		var input catalog.WorkflowToolBinding
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		input.Workflow = workflowName
		saved, err := s.cfg.CatalogStore.SaveWorkflowBinding(r.Context(), input)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		s.audit(r.Context(), p, "catalog.binding.patch", "workflow_tool_bindings", saved)
		writeJSON(w, http.StatusOK, saved)
	case http.MethodGet:
		binding, err := s.cfg.CatalogStore.GetWorkflowBinding(r.Context(), workflowName)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, binding)
	default:
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
	}
}
