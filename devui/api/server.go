package api

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/PipeOpsHQ/agent-sdk-go/framework/devui/auth"
	"github.com/PipeOpsHQ/agent-sdk-go/framework/devui/catalog"
	"github.com/PipeOpsHQ/agent-sdk-go/framework/flow"
	"github.com/PipeOpsHQ/agent-sdk-go/framework/observe"
	observestore "github.com/PipeOpsHQ/agent-sdk-go/framework/observe/store"
	cronpkg "github.com/PipeOpsHQ/agent-sdk-go/framework/runtime/cron"
	"github.com/PipeOpsHQ/agent-sdk-go/framework/state"
	fwtools "github.com/PipeOpsHQ/agent-sdk-go/framework/tools"
)

//go:embed static/*
var staticFiles embed.FS

type Config struct {
	Addr             string
	StateStore       state.Store
	TraceStore       observestore.Store
	CatalogStore     catalog.Store
	AuthStore        auth.Store
	AuditStore       AuditStore
	Runtime          RuntimeService
	Playground       PlaygroundRunner
	Scheduler        *cronpkg.Scheduler
	RequireAPIKey    bool
	AllowLocalNoAuth bool
	DefaultFlow      string
	WorkflowSpecDir  string
	ProviderEnvFile  string
	PromptSpecDir    string
}

type Server struct {
	cfg             Config
	stream          *eventStream
	mux             *http.ServeMux
	http            *http.Server
	once            sync.Once
	workerOverrides map[string]string
	overridesMu     sync.RWMutex
}

type principal struct {
	KeyID string
	Role  auth.Role
}

func NewServer(cfg Config) *Server {
	if strings.TrimSpace(cfg.Addr) == "" {
		cfg.Addr = "127.0.0.1:7070"
	}
	if strings.TrimSpace(cfg.WorkflowSpecDir) == "" {
		cfg.WorkflowSpecDir = "./.ai-agent/workflows"
	}
	if strings.TrimSpace(cfg.ProviderEnvFile) == "" {
		cfg.ProviderEnvFile = "./.ai-agent/provider_env.json"
	}
	if strings.TrimSpace(cfg.PromptSpecDir) == "" {
		cfg.PromptSpecDir = "./.ai-agent/prompts"
	}
	s := &Server{
		cfg:             cfg,
		stream:          newEventStream(),
		mux:             http.NewServeMux(),
		workerOverrides: map[string]string{},
	}
	s.registerRoutes()
	s.http = &http.Server{Addr: cfg.Addr, Handler: s.mux}
	return s
}

func (s *Server) Handler() http.Handler {
	if s == nil {
		return http.NotFoundHandler()
	}
	return s.mux
}

func (s *Server) ListenAndServe(ctx context.Context) error {
	if s == nil {
		return fmt.Errorf("server is nil")
	}
	errCh := make(chan error, 1)
	go func() {
		err := s.http.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case <-ctx.Done():
		log.Println("\n⏳ Shutdown signal received, gracefully stopping...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.http.Shutdown(shutdownCtx); err != nil {
			log.Printf("⚠️  HTTP shutdown error: %v", err)
		}
		log.Println("✅ Server stopped")
		return ctx.Err()
	case err := <-errCh:
		return err
	}
}

func (s *Server) Close() error {
	if s == nil {
		return nil
	}
	var outErr error
	s.once.Do(func() {
		log.Println("⏳ Closing server...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		outErr = s.http.Shutdown(shutdownCtx)
		if outErr != nil {
			log.Printf("⚠️  Server close error: %v", outErr)
		} else {
			log.Println("✅ Server closed")
		}
	})
	return outErr
}

func (s *Server) registerRoutes() {
	s.mux.HandleFunc("/api/v1/runs", s.require(auth.RoleViewer, s.handleRuns))
	s.mux.HandleFunc("/api/v1/runs/", s.require(auth.RoleViewer, s.handleRunSubresources))
	s.mux.HandleFunc("/api/v1/sessions/", s.require(auth.RoleViewer, s.handleSessionRuns))
	s.mux.HandleFunc("/api/v1/metrics/summary", s.require(auth.RoleViewer, s.handleMetrics))
	s.mux.HandleFunc("/api/v1/stream/events", s.require(auth.RoleViewer, s.handleSSE))
	s.mux.HandleFunc("/api/v1/events", s.require(auth.RoleOperator, s.handleIngestEvent))

	s.mux.HandleFunc("/api/v1/runtime/workers", s.require(auth.RoleViewer, s.handleRuntimeWorkers))
	s.mux.HandleFunc("/api/v1/runtime/workers/", s.require(auth.RoleViewer, s.handleRuntimeWorkerActions))
	s.mux.HandleFunc("/api/v1/runtime/queues", s.require(auth.RoleViewer, s.handleRuntimeQueues))
	s.mux.HandleFunc("/api/v1/runtime/queue-events", s.require(auth.RoleViewer, s.handleRuntimeQueueEvents))
	s.mux.HandleFunc("/api/v1/runtime/dlq", s.require(auth.RoleViewer, s.handleRuntimeDLQ))
	s.mux.HandleFunc("/api/v1/runtime/dlq/requeue", s.require(auth.RoleOperator, s.handleRuntimeDLQRequeue))
	s.mux.HandleFunc("/api/v1/runtime/details", s.require(auth.RoleViewer, s.handleRuntimeDetails))
	s.mux.HandleFunc("/api/v1/runtime/runs/", s.require(auth.RoleViewer, s.handleRuntimeRunActions))
	s.mux.HandleFunc("/api/v1/playground/run", s.require(auth.RoleViewer, s.handlePlaygroundRun))
	s.mux.HandleFunc("/api/v1/playground/stream", s.require(auth.RoleViewer, s.handlePlaygroundStream))
	s.mux.HandleFunc("/api/v1/commands/execute", s.require(auth.RoleViewer, s.handleCommandExecute))

	s.mux.HandleFunc("/api/v1/tools/registry", s.require(auth.RoleViewer, s.handleToolRegistry))
	s.mux.HandleFunc("/api/v1/tools/intelligence", s.require(auth.RoleViewer, s.handleToolIntelligence))
	s.mux.HandleFunc("/api/v1/tools/templates", s.require(auth.RoleViewer, s.handleToolTemplates))
	s.mux.HandleFunc("/api/v1/tools/instances", s.require(auth.RoleViewer, s.handleToolInstances))
	s.mux.HandleFunc("/api/v1/tools/instances/", s.require(auth.RoleViewer, s.handleToolInstanceByID))
	s.mux.HandleFunc("/api/v1/tools/bundles", s.require(auth.RoleViewer, s.handleToolBundles))
	s.mux.HandleFunc("/api/v1/tools/catalog", s.require(auth.RoleViewer, s.handleToolCatalog))
	s.mux.HandleFunc("/api/v1/workflows/registry", s.require(auth.RoleViewer, s.handleWorkflowRegistry))
	s.mux.HandleFunc("/api/v1/workflows", s.require(auth.RoleViewer, s.handleWorkflows))
	s.mux.HandleFunc("/api/v1/workflows/", s.require(auth.RoleViewer, s.handleWorkflowBindingByID))
	s.mux.HandleFunc("/api/v1/integrations/providers", s.require(auth.RoleViewer, s.handleIntegrationProviders))
	s.mux.HandleFunc("/api/v1/integrations/credentials", s.require(auth.RoleViewer, s.handleIntegrationCredentials))

	s.mux.HandleFunc("/api/v1/auth/keys", s.require(auth.RoleAdmin, s.handleAuthKeys))
	s.mux.HandleFunc("/api/v1/auth/keys/", s.require(auth.RoleAdmin, s.handleAuthKeyByID))
	s.mux.HandleFunc("/api/v1/auth/me", s.require(auth.RoleViewer, s.handleAuthMe))
	s.mux.HandleFunc("/api/v1/audit/logs", s.require(auth.RoleViewer, s.handleAuditLogs))
	s.mux.HandleFunc("/api/v1/cron/jobs", s.require(auth.RoleViewer, s.handleCronJobs))
	s.mux.HandleFunc("/api/v1/cron/jobs/", s.require(auth.RoleViewer, s.handleCronJobByName))
	s.mux.HandleFunc("/api/v1/skills", s.require(auth.RoleViewer, s.handleSkills))
	s.mux.HandleFunc("/api/v1/skills/", s.require(auth.RoleViewer, s.handleSkillByName))
	s.mux.HandleFunc("/api/v1/guardrails", s.require(auth.RoleViewer, s.handleGuardrails))
	s.mux.HandleFunc("/api/v1/flows", s.require(auth.RoleViewer, s.handleFlows))
	s.mux.HandleFunc("/api/v1/prompts", s.require(auth.RoleViewer, s.handlePrompts))
	s.mux.HandleFunc("/api/v1/prompts/", s.require(auth.RoleViewer, s.handlePromptByRef))
	s.mux.HandleFunc("/api/v1/prompts/render", s.require(auth.RoleViewer, s.handlePromptRender))
	s.mux.HandleFunc("/api/v1/prompts/validate", s.require(auth.RoleViewer, s.handlePromptValidate))
	s.mux.HandleFunc("/api/v1/files/view", s.require(auth.RoleViewer, s.handleFileView))
	s.mux.HandleFunc("/api/v1/files/download", s.require(auth.RoleViewer, s.handleFileDownload))
	s.mux.HandleFunc("/api/v1/reflect", s.require(auth.RoleViewer, s.handleReflect))
	s.mux.HandleFunc("/api/v1/actions/run", s.require(auth.RoleOperator, s.handleRunAction))
	s.mux.HandleFunc("/api/v1/settings/provider-env", s.require(auth.RoleViewer, s.handleProviderEnvSettings))
	s.mux.HandleFunc("/api/v1/settings/provider-models", s.require(auth.RoleOperator, s.handleProviderModelSettings))
	s.mux.HandleFunc("/api/v1/config", s.handleConfig)

	staticRoot, _ := fs.Sub(staticFiles, "static")
	files := http.FileServer(http.FS(staticRoot))
	s.mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}
		if r.URL.Path == "/" || r.URL.Path == "" {
			http.ServeFileFS(w, r, staticRoot, "index.html")
			return
		}
		files.ServeHTTP(w, r)
	})
}

func (s *Server) require(minRole auth.Role, h func(http.ResponseWriter, *http.Request, principal)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, err := s.authenticate(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, err)
			return
		}
		if p.Role.Rank() < minRole.Rank() {
			writeError(w, http.StatusForbidden, fmt.Errorf("insufficient role: requires %s", minRole))
			return
		}
		h(w, r, p)
	}
}

func (s *Server) authenticate(r *http.Request) (principal, error) {
	if r == nil {
		return principal{}, fmt.Errorf("request is nil")
	}
	key := extractAPIKey(r)
	if key == "" {
		if !s.cfg.RequireAPIKey && s.cfg.AllowLocalNoAuth && isLocalRequest(r.RemoteAddr) {
			return principal{KeyID: "local", Role: auth.RoleAdmin}, nil
		}
		return principal{}, fmt.Errorf("missing API key")
	}
	if s.cfg.AuthStore == nil {
		return principal{}, fmt.Errorf("auth store is not configured")
	}
	k, err := s.cfg.AuthStore.VerifyKey(r.Context(), key)
	if err != nil {
		return principal{}, err
	}
	return principal{KeyID: k.ID, Role: k.Role}, nil
}

func extractAPIKey(r *http.Request) string {
	if r == nil {
		return ""
	}
	if key := strings.TrimSpace(r.Header.Get("X-API-Key")); key != "" {
		return key
	}
	if authz := strings.TrimSpace(r.Header.Get("Authorization")); strings.HasPrefix(strings.ToLower(authz), "bearer ") {
		return strings.TrimSpace(authz[7:])
	}
	if key := strings.TrimSpace(r.URL.Query().Get("api_key")); key != "" {
		return key
	}
	return ""
}

func isLocalRequest(remoteAddr string) bool {
	host := remoteAddr
	if h, _, err := net.SplitHostPort(remoteAddr); err == nil {
		host = h
	}
	ip := net.ParseIP(strings.TrimSpace(host))
	if ip != nil {
		return ip.IsLoopback()
	}
	return host == "localhost"
}

func (s *Server) handleRuns(w http.ResponseWriter, r *http.Request, _ principal) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
		return
	}
	if s.cfg.StateStore == nil {
		writeJSON(w, http.StatusOK, []state.RunRecord{})
		return
	}
	q := state.ListRunsQuery{
		SessionID: strings.TrimSpace(r.URL.Query().Get("session_id")),
		Status:    strings.TrimSpace(r.URL.Query().Get("status")),
		Limit:     parseInt(r.URL.Query().Get("limit"), 100),
		Offset:    parseInt(r.URL.Query().Get("offset"), 0),
	}
	runs, err := s.cfg.StateStore.ListRuns(r.Context(), q)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, runs)
}

func (s *Server) handleRunSubresources(w http.ResponseWriter, r *http.Request, p principal) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/runs/")
	parts := splitPath(path)
	if len(parts) == 0 {
		writeError(w, http.StatusNotFound, fmt.Errorf("run id is required"))
		return
	}
	runID := parts[0]
	if len(parts) == 1 {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
			return
		}
		if s.cfg.StateStore == nil {
			writeError(w, http.StatusNotImplemented, fmt.Errorf("state store not configured"))
			return
		}
		run, err := s.cfg.StateStore.LoadRun(r.Context(), runID)
		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, state.ErrNotFound) {
				status = http.StatusNotFound
			}
			writeError(w, status, err)
			return
		}
		writeJSON(w, http.StatusOK, run)
		return
	}

	switch parts[1] {
	case "events":
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
			return
		}
		if s.cfg.TraceStore == nil {
			writeJSON(w, http.StatusOK, []observe.Event{})
			return
		}
		events, err := s.cfg.TraceStore.ListEventsByRun(r.Context(), runID, observestore.ListQuery{
			Limit:  parseInt(r.URL.Query().Get("limit"), 500),
			Offset: parseInt(r.URL.Query().Get("offset"), 0),
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, events)
	case "checkpoints":
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
			return
		}
		if s.cfg.StateStore == nil {
			writeJSON(w, http.StatusOK, []state.CheckpointRecord{})
			return
		}
		rows, err := s.cfg.StateStore.ListCheckpoints(r.Context(), runID, parseInt(r.URL.Query().Get("limit"), 100))
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, rows)
	case "interventions":
		if s.cfg.StateStore == nil {
			writeError(w, http.StatusNotImplemented, fmt.Errorf("state store not configured"))
			return
		}
		switch r.Method {
		case http.MethodGet:
			run, err := s.cfg.StateStore.LoadRun(r.Context(), runID)
			if err != nil {
				status := http.StatusInternalServerError
				if errors.Is(err, state.ErrNotFound) {
					status = http.StatusNotFound
				}
				writeError(w, status, err)
				return
			}
			writeJSON(w, http.StatusOK, s.listRunInterventions(run))
		case http.MethodPost:
			if p.Role.Rank() < auth.RoleOperator.Rank() {
				writeError(w, http.StatusForbidden, fmt.Errorf("insufficient role: requires %s", auth.RoleOperator))
				return
			}
			var req interventionRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				writeError(w, http.StatusBadRequest, err)
				return
			}
			entry, err := s.applyIntervention(r.Context(), p, runID, req)
			if err != nil {
				writeError(w, http.StatusBadRequest, err)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"ok":    true,
				"entry": entry,
			})
		default:
			writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
		}
	default:
		writeError(w, http.StatusNotFound, fmt.Errorf("unsupported run endpoint"))
	}
}

func (s *Server) handleSessionRuns(w http.ResponseWriter, r *http.Request, _ principal) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
		return
	}
	parts := splitPath(strings.TrimPrefix(r.URL.Path, "/api/v1/sessions/"))
	if len(parts) != 2 || parts[1] != "runs" {
		writeError(w, http.StatusNotFound, fmt.Errorf("unsupported session endpoint"))
		return
	}
	if s.cfg.StateStore == nil {
		writeJSON(w, http.StatusOK, []state.RunRecord{})
		return
	}
	runs, err := s.cfg.StateStore.ListRuns(r.Context(), state.ListRunsQuery{
		SessionID: parts[0],
		Limit:     parseInt(r.URL.Query().Get("limit"), 100),
		Offset:    parseInt(r.URL.Query().Get("offset"), 0),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, runs)
}

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request, _ principal) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
		return
	}
	if s.cfg.TraceStore == nil {
		writeJSON(w, http.StatusOK, observestore.MetricsSummary{})
		return
	}
	metrics, err := s.cfg.TraceStore.AggregateMetrics(r.Context(), observestore.MetricsQuery{})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, metrics)
}

func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request, _ principal) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("streaming unsupported"))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	id, ch := s.stream.subscribe(128)
	defer s.stream.unsubscribe(id)
	runIDFilter := strings.TrimSpace(r.URL.Query().Get("run_id"))
	kindFilter := strings.TrimSpace(r.URL.Query().Get("kind"))
	statusFilter := strings.TrimSpace(r.URL.Query().Get("status"))

	if s.cfg.TraceStore != nil && runIDFilter != "" {
		backlog, err := s.cfg.TraceStore.ListEventsByRun(r.Context(), runIDFilter, observestore.ListQuery{Limit: 50, Offset: 0})
		if err == nil {
			for _, event := range backlog {
				if !eventMatchesFilter(event, runIDFilter, kindFilter, statusFilter) {
					continue
				}
				payload, _ := json.Marshal(event)
				_, _ = w.Write([]byte("data: "))
				_, _ = w.Write(payload)
				_, _ = w.Write([]byte("\n\n"))
			}
			flusher.Flush()
		}
	}

	ping := time.NewTicker(15 * time.Second)
	defer ping.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ping.C:
			if _, err := w.Write([]byte(": keepalive\n\n")); err != nil {
				return // client disconnected
			}
			flusher.Flush()
		case event := <-ch:
			if !eventMatchesFilter(event, runIDFilter, kindFilter, statusFilter) {
				continue
			}
			payload, _ := json.Marshal(event)
			if _, err := w.Write([]byte("data: ")); err != nil {
				return
			}
			if _, err := w.Write(payload); err != nil {
				return
			}
			if _, err := w.Write([]byte("\n\n")); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func (s *Server) handleIngestEvent(w http.ResponseWriter, r *http.Request, p principal) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
		return
	}
	if s.cfg.TraceStore == nil {
		writeError(w, http.StatusNotImplemented, fmt.Errorf("trace store not configured"))
		return
	}
	var event observe.Event
	if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("invalid event payload: %w", err))
		return
	}
	event.Normalize()
	if err := s.cfg.TraceStore.SaveEvent(r.Context(), event); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	s.stream.publish(event)
	s.audit(r.Context(), p, "observe.event.ingest", "trace_events", event)
	writeJSON(w, http.StatusAccepted, map[string]any{"ok": true})
}

func (s *Server) handleRuntimeWorkers(w http.ResponseWriter, r *http.Request, _ principal) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
		return
	}
	if s.cfg.Runtime == nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}
	workers, err := s.cfg.Runtime.ListWorkers(r.Context(), parseInt(r.URL.Query().Get("limit"), 100))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	workers = s.withWorkerOverrides(workers)
	writeJSON(w, http.StatusOK, workers)
}

func (s *Server) handleRuntimeQueues(w http.ResponseWriter, r *http.Request, _ principal) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
		return
	}
	if s.cfg.Runtime == nil {
		writeJSON(w, http.StatusOK, map[string]any{"streamLength": 0, "pending": 0, "dlqLength": 0})
		return
	}
	stats, err := s.cfg.Runtime.QueueStats(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

func (s *Server) handleRuntimeDLQ(w http.ResponseWriter, r *http.Request, _ principal) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
		return
	}
	if s.cfg.Runtime == nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}
	dlq, err := s.cfg.Runtime.ListDLQ(r.Context(), parseInt(r.URL.Query().Get("limit"), 100))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, dlq)
}

func (s *Server) handleRuntimeDetails(w http.ResponseWriter, r *http.Request, _ principal) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
		return
	}
	response := map[string]any{
		"available": false,
		"status":    "unavailable",
		"queue": map[string]any{
			"streamLength": 0,
			"pending":      0,
			"dlqLength":    0,
		},
		"workers":     []any{},
		"workerCount": 0,
		"dlqCount":    0,
	}
	if s.cfg.Runtime == nil {
		response["error"] = "runtime service not configured"
		writeJSON(w, http.StatusOK, response)
		return
	}

	errorsByArea := map[string]string{}
	if queueStats, err := s.cfg.Runtime.QueueStats(r.Context()); err == nil {
		response["queue"] = queueStats
	} else {
		errorsByArea["queue"] = err.Error()
	}

	if workers, err := s.cfg.Runtime.ListWorkers(r.Context(), 100); err == nil {
		workers = s.withWorkerOverrides(workers)
		response["workers"] = workers
		response["workerCount"] = len(workers)
	} else {
		errorsByArea["workers"] = err.Error()
	}

	if dlq, err := s.cfg.Runtime.ListDLQ(r.Context(), 100); err == nil {
		response["dlq"] = dlq
		response["dlqCount"] = len(dlq)
	} else {
		errorsByArea["dlq"] = err.Error()
	}

	response["available"] = true
	if len(errorsByArea) == 0 {
		response["status"] = "healthy"
	} else {
		response["status"] = "degraded"
		response["errors"] = errorsByArea
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleRuntimeRunActions(w http.ResponseWriter, r *http.Request, p principal) {
	if s.cfg.Runtime == nil {
		writeError(w, http.StatusNotImplemented, fmt.Errorf("runtime service not configured"))
		return
	}
	parts := splitPath(strings.TrimPrefix(r.URL.Path, "/api/v1/runtime/runs/"))
	if len(parts) != 2 {
		writeError(w, http.StatusNotFound, fmt.Errorf("unsupported runtime run endpoint"))
		return
	}
	runID := parts[0]
	action := parts[1]
	switch action {
	case "attempts":
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
			return
		}
		attempts, err := s.cfg.Runtime.ListRunAttempts(r.Context(), runID, parseInt(r.URL.Query().Get("limit"), 100))
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, attempts)
	case "cancel":
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
			return
		}
		if p.Role.Rank() < auth.RoleOperator.Rank() {
			writeError(w, http.StatusForbidden, fmt.Errorf("insufficient role: requires %s", auth.RoleOperator))
			return
		}
		if err := s.cfg.Runtime.CancelRun(r.Context(), runID); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		s.audit(r.Context(), p, "runtime.run.cancel", "runs", map[string]any{"runId": runID})
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	case "requeue":
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
			return
		}
		if p.Role.Rank() < auth.RoleOperator.Rank() {
			writeError(w, http.StatusForbidden, fmt.Errorf("insufficient role: requires %s", auth.RoleOperator))
			return
		}
		if err := s.cfg.Runtime.RequeueRun(r.Context(), runID); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		s.audit(r.Context(), p, "runtime.run.requeue", "runs", map[string]any{"runId": runID})
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	default:
		writeError(w, http.StatusNotFound, fmt.Errorf("unsupported runtime run action"))
	}
}

func (s *Server) handlePlaygroundRun(w http.ResponseWriter, r *http.Request, _ principal) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
		return
	}
	if s.cfg.Playground == nil {
		writeError(w, http.StatusNotImplemented, fmt.Errorf("playground runner not configured"))
		return
	}
	var req PlaygroundRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	resp, err := s.cfg.Playground.Run(r.Context(), req)
	if err != nil {
		writeJSON(w, http.StatusOK, PlaygroundResponse{
			Status: "failed",
			Error:  err.Error(),
		})
		return
	}
	if strings.TrimSpace(resp.Status) == "" {
		resp.Status = "completed"
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handlePlaygroundStream(w http.ResponseWriter, r *http.Request, _ principal) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
		return
	}
	if s.cfg.Playground == nil {
		writeError(w, http.StatusNotImplemented, fmt.Errorf("playground runner not configured"))
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("streaming unsupported"))
		return
	}

	var req PlaygroundRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	// Subscribe to the event stream to capture events during this run
	subID, eventCh := s.stream.subscribe(64)
	defer s.stream.unsubscribe(subID)

	// Run playground in a goroutine, stream events as they arrive
	type runResult struct {
		resp PlaygroundResponse
		err  error
	}
	done := make(chan runResult, 1)
	go func() {
		resp, err := s.cfg.Playground.Run(r.Context(), req)
		done <- runResult{resp, err}
	}()

	sendSSE := func(eventType string, data any) {
		b, _ := json.Marshal(data)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventType, b)
		flusher.Flush()
	}

	// Stream events until the run completes
	for {
		select {
		case event := <-eventCh:
			sendSSE("progress", map[string]any{
				"kind":     event.Kind,
				"status":   event.Status,
				"name":     event.Name,
				"message":  event.Message,
				"toolName": event.ToolName,
				"runId":    event.RunID,
			})
		case result := <-done:
			// Drain any remaining events
			for {
				select {
				case event := <-eventCh:
					sendSSE("progress", map[string]any{
						"kind":     event.Kind,
						"status":   event.Status,
						"name":     event.Name,
						"message":  event.Message,
						"toolName": event.ToolName,
						"runId":    event.RunID,
					})
				default:
					goto drained
				}
			}
		drained:
			if result.err != nil {
				sendSSE("complete", PlaygroundResponse{
					Status: "failed",
					Error:  result.err.Error(),
				})
			} else {
				resp := result.resp
				if strings.TrimSpace(resp.Status) == "" {
					resp.Status = "completed"
				}
				if out := strings.TrimSpace(resp.Output); out != "" {
					for _, chunk := range splitOutputChunks(out, 180) {
						sendSSE("delta", map[string]any{"text": chunk})
					}
				}
				sendSSE("complete", resp)
			}
			return
		case <-r.Context().Done():
			return
		}
	}
}

func splitOutputChunks(s string, n int) []string {
	if n <= 0 {
		n = 180
	}
	runes := []rune(s)
	if len(runes) <= n {
		return []string{s}
	}
	out := make([]string, 0, (len(runes)/n)+1)
	for i := 0; i < len(runes); i += n {
		end := i + n
		if end > len(runes) {
			end = len(runes)
		}
		out = append(out, string(runes[i:end]))
	}
	return out
}

func (s *Server) handleToolTemplates(w http.ResponseWriter, r *http.Request, p principal) {
	if s.cfg.CatalogStore == nil {
		writeError(w, http.StatusNotImplemented, fmt.Errorf("catalog store not configured"))
		return
	}
	switch r.Method {
	case http.MethodGet:
		items, err := s.cfg.CatalogStore.ListTemplates(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, items)
	case http.MethodPost:
		if p.Role.Rank() < auth.RoleOperator.Rank() {
			writeError(w, http.StatusForbidden, fmt.Errorf("insufficient role: requires %s", auth.RoleOperator))
			return
		}
		var item catalog.ToolTemplate
		if err := json.NewDecoder(r.Body).Decode(&item); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		saved, err := s.cfg.CatalogStore.SaveTemplate(r.Context(), item)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		s.audit(r.Context(), p, "catalog.template.save", "tool_templates", saved)
		writeJSON(w, http.StatusCreated, saved)
	default:
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
	}
}

func (s *Server) handleToolRegistry(w http.ResponseWriter, r *http.Request, _ principal) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
		return
	}
	tools := fwtools.ToolCatalog()
	bundles := fwtools.BundleCatalog()
	writeJSON(w, http.StatusOK, map[string]any{
		"tools":       tools,
		"bundles":     bundles,
		"toolCount":   len(tools),
		"bundleCount": len(bundles),
	})
}

func (s *Server) handleToolInstances(w http.ResponseWriter, r *http.Request, p principal) {
	if s.cfg.CatalogStore == nil {
		writeError(w, http.StatusNotImplemented, fmt.Errorf("catalog store not configured"))
		return
	}
	switch r.Method {
	case http.MethodGet:
		items, err := s.cfg.CatalogStore.ListInstances(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, items)
	case http.MethodPost:
		if p.Role.Rank() < auth.RoleOperator.Rank() {
			writeError(w, http.StatusForbidden, fmt.Errorf("insufficient role: requires %s", auth.RoleOperator))
			return
		}
		var item catalog.ToolInstance
		if err := json.NewDecoder(r.Body).Decode(&item); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		saved, err := s.cfg.CatalogStore.SaveInstance(r.Context(), item)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		s.audit(r.Context(), p, "catalog.instance.save", "tool_instances", saved)
		writeJSON(w, http.StatusCreated, saved)
	default:
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
	}
}

func (s *Server) handleToolInstanceByID(w http.ResponseWriter, r *http.Request, p principal) {
	if s.cfg.CatalogStore == nil {
		writeError(w, http.StatusNotImplemented, fmt.Errorf("catalog store not configured"))
		return
	}
	id := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/api/v1/tools/instances/"))
	if id == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("instance id is required"))
		return
	}
	switch r.Method {
	case http.MethodPatch:
		if p.Role.Rank() < auth.RoleOperator.Rank() {
			writeError(w, http.StatusForbidden, fmt.Errorf("insufficient role: requires %s", auth.RoleOperator))
			return
		}
		var item catalog.ToolInstance
		if err := json.NewDecoder(r.Body).Decode(&item); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		item.ID = id
		saved, err := s.cfg.CatalogStore.SaveInstance(r.Context(), item)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		s.audit(r.Context(), p, "catalog.instance.patch", "tool_instances", saved)
		writeJSON(w, http.StatusOK, saved)
	case http.MethodDelete:
		if p.Role.Rank() < auth.RoleOperator.Rank() {
			writeError(w, http.StatusForbidden, fmt.Errorf("insufficient role: requires %s", auth.RoleOperator))
			return
		}
		if err := s.cfg.CatalogStore.DeleteInstance(r.Context(), id); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		s.audit(r.Context(), p, "catalog.instance.delete", "tool_instances", map[string]any{"id": id})
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	default:
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
	}
}

func (s *Server) handleToolBundles(w http.ResponseWriter, r *http.Request, p principal) {
	if s.cfg.CatalogStore == nil {
		writeError(w, http.StatusNotImplemented, fmt.Errorf("catalog store not configured"))
		return
	}
	switch r.Method {
	case http.MethodGet:
		items, err := s.cfg.CatalogStore.ListBundles(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, items)
	case http.MethodPost:
		if p.Role.Rank() < auth.RoleOperator.Rank() {
			writeError(w, http.StatusForbidden, fmt.Errorf("insufficient role: requires %s", auth.RoleOperator))
			return
		}
		var item catalog.ToolBundle
		if err := json.NewDecoder(r.Body).Decode(&item); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		saved, err := s.cfg.CatalogStore.SaveBundle(r.Context(), item)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		s.audit(r.Context(), p, "catalog.bundle.save", "tool_bundles", saved)
		writeJSON(w, http.StatusCreated, saved)
	default:
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
	}
}

func (s *Server) handleToolCatalog(w http.ResponseWriter, r *http.Request, _ principal) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
		return
	}
	type bundleItem struct {
		Name        string   `json:"name"`
		Description string   `json:"description"`
		Tools       []string `json:"tools"`
	}
	type toolItem struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	bundles := fwtools.BundleCatalog()
	bi := make([]bundleItem, len(bundles))
	for i, b := range bundles {
		bi[i] = bundleItem{Name: "@" + b.Name, Description: b.Description, Tools: b.Tools}
	}
	tc := fwtools.ToolCatalog()
	ti := make([]toolItem, len(tc))
	for i, t := range tc {
		ti[i] = toolItem{Name: t.Name, Description: t.Description}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"bundles": bi,
		"tools":   ti,
	})
}

func (s *Server) handleFlows(w http.ResponseWriter, r *http.Request, _ principal) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
		return
	}
	defs := flow.All()
	writeJSON(w, http.StatusOK, map[string]any{
		"flows": defs,
		"count": len(defs),
	})
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"defaultFlow": s.cfg.DefaultFlow,
	})
}

func (s *Server) handleIntegrationProviders(w http.ResponseWriter, r *http.Request, p principal) {
	if s.cfg.CatalogStore == nil {
		writeError(w, http.StatusNotImplemented, fmt.Errorf("catalog store not configured"))
		return
	}
	switch r.Method {
	case http.MethodGet:
		items, err := s.cfg.CatalogStore.ListIntegrationProviders(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, items)
	case http.MethodPost:
		if p.Role.Rank() < auth.RoleOperator.Rank() {
			writeError(w, http.StatusForbidden, fmt.Errorf("insufficient role: requires %s", auth.RoleOperator))
			return
		}
		var provider catalog.IntegrationProvider
		if err := json.NewDecoder(r.Body).Decode(&provider); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		saved, err := s.cfg.CatalogStore.SaveIntegrationProvider(r.Context(), provider)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		s.audit(r.Context(), p, "catalog.integration_provider.save", "integration_providers", saved)
		writeJSON(w, http.StatusCreated, saved)
	default:
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
	}
}

func (s *Server) handleIntegrationCredentials(w http.ResponseWriter, r *http.Request, p principal) {
	if s.cfg.CatalogStore == nil {
		writeError(w, http.StatusNotImplemented, fmt.Errorf("catalog store not configured"))
		return
	}
	switch r.Method {
	case http.MethodGet:
		provider := strings.TrimSpace(r.URL.Query().Get("provider"))
		items, err := s.cfg.CatalogStore.ListCredentialMeta(r.Context(), provider)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, items)
	case http.MethodPost:
		if p.Role.Rank() < auth.RoleOperator.Rank() {
			writeError(w, http.StatusForbidden, fmt.Errorf("insufficient role: requires %s", auth.RoleOperator))
			return
		}
		var meta catalog.IntegrationCredentialMeta
		if err := json.NewDecoder(r.Body).Decode(&meta); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		saved, err := s.cfg.CatalogStore.SaveCredentialMeta(r.Context(), meta)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		s.audit(r.Context(), p, "catalog.integration_credential.save", "integration_credentials_meta", saved)
		writeJSON(w, http.StatusCreated, saved)
	default:
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
	}
}

func (s *Server) handleAuthKeys(w http.ResponseWriter, r *http.Request, p principal) {
	if s.cfg.AuthStore == nil {
		writeError(w, http.StatusNotImplemented, fmt.Errorf("auth store not configured"))
		return
	}
	switch r.Method {
	case http.MethodGet:
		keys, err := s.cfg.AuthStore.ListKeys(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, keys)
	case http.MethodPost:
		var input struct {
			Role auth.Role `json:"role"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if input.Role == "" {
			input.Role = auth.RoleViewer
		}
		key, err := s.cfg.AuthStore.CreateKey(r.Context(), input.Role)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		s.audit(r.Context(), p, "auth.key.create", "api_keys", map[string]any{"id": key.ID, "role": key.Role})
		writeJSON(w, http.StatusCreated, key)
	default:
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
	}
}

func (s *Server) handleAuthMe(w http.ResponseWriter, r *http.Request, p principal) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"keyId": p.KeyID,
		"role":  p.Role,
	})
}

func (s *Server) handleAuthKeyByID(w http.ResponseWriter, r *http.Request, p principal) {
	if s.cfg.AuthStore == nil {
		writeError(w, http.StatusNotImplemented, fmt.Errorf("auth store not configured"))
		return
	}
	id := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/api/v1/auth/keys/"))
	if id == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("key id is required"))
		return
	}
	if r.Method != http.MethodDelete {
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
		return
	}
	if err := s.cfg.AuthStore.DisableKey(r.Context(), id); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	s.audit(r.Context(), p, "auth.key.disable", "api_keys", map[string]any{"id": id})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) audit(ctx context.Context, p principal, action string, resource string, payload any) {
	if s == nil || s.cfg.AuditStore == nil {
		return
	}
	body, _ := json.Marshal(payload)
	_ = s.cfg.AuditStore.Record(ctx, AuditLog{
		ActorKeyID: p.KeyID,
		Action:     action,
		Resource:   resource,
		Payload:    string(body),
	})
}

func (s *Server) Emit(event observe.Event) {
	event.Normalize()
	s.stream.publish(event)
}

func splitPath(path string) []string {
	trimmed := strings.Trim(path, "/")
	if trimmed == "" {
		return nil
	}
	parts := strings.Split(trimmed, "/")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		out = append(out, p)
	}
	return out
}

func parseInt(raw string, fallback int) int {
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return fallback
	}
	return n
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, err error) {
	msg := "unknown error"
	if err != nil {
		msg = err.Error()
	}
	writeJSON(w, status, map[string]any{"error": msg})
}
