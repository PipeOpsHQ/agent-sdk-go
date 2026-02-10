package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/PipeOpsHQ/agent-sdk-go/delivery"
	"github.com/PipeOpsHQ/agent-sdk-go/devui/auth"
	cronpkg "github.com/PipeOpsHQ/agent-sdk-go/runtime/cron"
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
		req.Config.ReplyTo = delivery.Normalize(req.Config.ReplyTo)
		if err := s.cfg.Scheduler.Add(req.Name, req.CronExpr, req.Config); err != nil {
			writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		}
		job, _ := s.cfg.Scheduler.Get(req.Name)
		auditPayload := map[string]any{"name": req.Name, "cronExpr": req.CronExpr}
		if req.Config.ReplyTo != nil {
			auditPayload["channel"] = req.Config.ReplyTo.Channel
			auditPayload["destination"] = req.Config.ReplyTo.Destination
			auditPayload["threadId"] = req.Config.ReplyTo.ThreadID
			auditPayload["userId"] = req.Config.ReplyTo.UserID
		}
		s.audit(r.Context(), p, "cron.create", "cron", auditPayload)
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
	case r.Method == http.MethodGet && action == "history":
		history, err := s.cfg.Scheduler.History(name, parseInt(r.URL.Query().Get("limit"), 50))
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"name": name, "runs": history, "count": len(history)})

	case r.Method == http.MethodGet && action == "logs":
		history, err := s.cfg.Scheduler.History(name, parseInt(r.URL.Query().Get("limit"), 50))
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"name": name, "logs": history, "count": len(history)})

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
		output, err := s.cfg.Scheduler.Trigger(name)
		status := "completed"
		if err != nil {
			status = "failed"
		}
		auditPayload := map[string]any{"name": name, "status": status}
		if err != nil {
			auditPayload["error"] = err.Error()
		}
		s.audit(r.Context(), p, "cron.trigger", "cron", auditPayload)
		writeJSON(w, http.StatusOK, map[string]any{
			"name":   name,
			"status": status,
			"output": output,
			"error":  errorString(err),
		})

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

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
