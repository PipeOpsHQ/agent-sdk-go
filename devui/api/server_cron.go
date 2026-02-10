package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/PipeOpsHQ/agent-sdk-go/framework/devui/auth"
	cronpkg "github.com/PipeOpsHQ/agent-sdk-go/framework/runtime/cron"
)

func (s *Server) handleCronJobs(w http.ResponseWriter, r *http.Request, p principal) {
	if s.cfg.Scheduler == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "scheduler not configured"})
		return
	}

	switch r.Method {
	case http.MethodGet:
		jobs := s.cfg.Scheduler.List()
		writeJSON(w, http.StatusOK, jobs)

	case http.MethodPost:
		if p.Role.Rank() < auth.RoleOperator.Rank() {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "insufficient role: requires operator"})
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "read body: " + err.Error()})
			return
		}
		var req struct {
			Name     string            `json:"name"`
			CronExpr string            `json:"cronExpr"`
			Config   cronpkg.JobConfig `json:"config"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
			return
		}
		if err := s.cfg.Scheduler.Add(req.Name, req.CronExpr, req.Config); err != nil {
			writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		}
		job, _ := s.cfg.Scheduler.Get(req.Name)
		s.audit(r.Context(), p, "cron.create", "cron", map[string]any{"name": req.Name, "cronExpr": req.CronExpr})
		writeJSON(w, http.StatusCreated, job)

	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleCronJobByName(w http.ResponseWriter, r *http.Request, p principal) {
	if s.cfg.Scheduler == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "scheduler not configured"})
		return
	}

	parts := splitPath(strings.TrimPrefix(r.URL.Path, "/api/v1/cron/jobs/"))
	if len(parts) == 0 || parts[0] == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "job name required"})
		return
	}
	name := parts[0]
	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}

	switch {
	case r.Method == http.MethodGet:
		job, ok := s.cfg.Scheduler.Get(name)
		if !ok {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "job not found"})
			return
		}
		writeJSON(w, http.StatusOK, job)

	case r.Method == http.MethodDelete:
		if p.Role.Rank() < auth.RoleOperator.Rank() {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "insufficient role: requires operator"})
			return
		}
		if err := s.cfg.Scheduler.Remove(name); err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
		s.audit(r.Context(), p, "cron.delete", "cron", map[string]any{"name": name})
		writeJSON(w, http.StatusOK, map[string]string{"status": "removed"})

	case r.Method == http.MethodPost && action == "trigger":
		if p.Role.Rank() < auth.RoleOperator.Rank() {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "insufficient role: requires operator"})
			return
		}
		job, ok := s.cfg.Scheduler.Get(name)
		if !ok {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "job not found"})
			return
		}
		if s.cfg.Playground == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "playground runner not configured"})
			return
		}
		resp, err := s.cfg.Playground.Run(r.Context(), PlaygroundRequest{
			Input:        job.Config.Input,
			Workflow:     job.Config.Workflow,
			Tools:        job.Config.Tools,
			SystemPrompt: job.Config.SystemPrompt,
		})
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		s.audit(r.Context(), p, "cron.trigger", "cron", map[string]any{"name": name})
		writeJSON(w, http.StatusOK, resp)

	case r.Method == http.MethodPatch:
		if p.Role.Rank() < auth.RoleOperator.Rank() {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "insufficient role: requires operator"})
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "read body: " + err.Error()})
			return
		}
		var patch struct {
			Enabled *bool `json:"enabled"`
		}
		if err := json.Unmarshal(body, &patch); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
			return
		}
		if patch.Enabled != nil {
			if err := s.cfg.Scheduler.SetEnabled(name, *patch.Enabled); err != nil {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
				return
			}
		}
		job, _ := s.cfg.Scheduler.Get(name)
		s.audit(r.Context(), p, "cron.update", "cron", map[string]any{"name": name})
		writeJSON(w, http.StatusOK, job)

	default:
		w.Header().Set("Allow", "GET, DELETE, POST, PATCH")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
