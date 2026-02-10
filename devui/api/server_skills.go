package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/PipeOpsHQ/agent-sdk-go/devui/auth"
	"github.com/PipeOpsHQ/agent-sdk-go/skill"
)

// handleSkills handles GET /api/v1/skills (list) and POST /api/v1/skills (install from GitHub).
func (s *Server) handleSkills(w http.ResponseWriter, r *http.Request, p principal) {
	switch r.Method {
	case http.MethodGet:
		s.handleSkillsList(w, r)
	case http.MethodPost:
		if p.Role.Rank() < auth.RoleOperator.Rank() {
			writeError(w, http.StatusForbidden, fmt.Errorf("insufficient role: requires %s", auth.RoleOperator))
			return
		}
		s.handleSkillsInstall(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
	}
}

func (s *Server) handleSkillsList(w http.ResponseWriter, _ *http.Request) {
	skills := skill.All()
	out := make([]map[string]any, 0, len(skills))
	for _, sk := range skills {
		out = append(out, map[string]any{
			"name":         sk.Name,
			"description":  sk.Description,
			"license":      sk.License,
			"allowedTools": sk.AllowedTools,
			"metadata":     sk.Metadata,
			"source":       sk.Source,
			"path":         sk.Path,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"skills": out, "count": len(out)})
}

type skillInstallRequest struct {
	RepoURL string `json:"repoUrl"`
	DestDir string `json:"destDir,omitempty"`
}

func (s *Server) handleSkillsInstall(w http.ResponseWriter, r *http.Request) {
	var req skillInstallRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("invalid request body: %w", err))
		return
	}
	if req.RepoURL == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("repoUrl is required"))
		return
	}
	destDir := req.DestDir
	if destDir == "" {
		destDir = "./skills"
	}

	count, err := skill.InstallFromGitHub(req.RepoURL, destDir)
	if err != nil {
		msg := strings.ToLower(err.Error())
		status := http.StatusInternalServerError
		if strings.Contains(msg, "invalid repo") || strings.Contains(msg, "unsupported repo host") || strings.Contains(msg, "no skills found") {
			status = http.StatusBadRequest
		}
		writeError(w, status, fmt.Errorf("install failed: %w", err))
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "installed",
		"count":   count,
		"repoUrl": req.RepoURL,
	})
}

// handleSkillByName handles GET/DELETE /api/v1/skills/{name}
func (s *Server) handleSkillByName(w http.ResponseWriter, r *http.Request, p principal) {
	name := strings.TrimPrefix(r.URL.Path, "/api/v1/skills/")
	if name == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("skill name is required"))
		return
	}
	switch r.Method {
	case http.MethodGet:
		sk, ok := skill.Get(name)
		if !ok {
			writeError(w, http.StatusNotFound, fmt.Errorf("skill %q not found", name))
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"name":         sk.Name,
			"description":  sk.Description,
			"license":      sk.License,
			"allowedTools": sk.AllowedTools,
			"metadata":     sk.Metadata,
			"instructions": sk.Instructions,
			"source":       sk.Source,
			"path":         sk.Path,
		})
	case http.MethodDelete:
		if p.Role.Rank() < auth.RoleOperator.Rank() {
			writeError(w, http.StatusForbidden, fmt.Errorf("insufficient role: requires %s", auth.RoleOperator))
			return
		}
		if !skill.Remove(name) {
			writeError(w, http.StatusNotFound, fmt.Errorf("skill %q not found", name))
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "removed", "name": name})
	default:
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
	}
}
