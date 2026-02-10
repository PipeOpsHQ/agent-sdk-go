package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/PipeOpsHQ/agent-sdk-go/framework/devui/auth"
)

type providerEnvUpdateRequest struct {
	Values map[string]string `json:"values"`
}

func (s *Server) handleProviderEnvSettings(w http.ResponseWriter, r *http.Request, p principal) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, buildProviderEnvResponse())
	case http.MethodPut:
		if p.Role.Rank() < auth.RoleOperator.Rank() {
			writeError(w, http.StatusForbidden, fmt.Errorf("insufficient role: requires %s", auth.RoleOperator))
			return
		}
		var req providerEnvUpdateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		meta := providerEnvMeta()
		existing, err := loadProviderEnvFile(s.cfg.ProviderEnvFile)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		if existing == nil {
			existing = map[string]string{}
		}
		for key, value := range req.Values {
			if _, ok := meta[key]; !ok {
				writeError(w, http.StatusBadRequest, fmt.Errorf("unsupported provider setting %q", key))
				return
			}
			trimmed := strings.TrimSpace(value)
			if trimmed == "" {
				delete(existing, key)
				continue
			}
			existing[key] = trimmed
		}
		if err := saveProviderEnvFile(s.cfg.ProviderEnvFile, existing); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		applyProviderEnv(existing)
		response := buildProviderEnvResponse()
		s.audit(r.Context(), p, "settings.provider_env.update", "provider_env", req.Values)
		writeJSON(w, http.StatusOK, response)
	default:
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
	}
}

func (s *Server) handleProviderModelSettings(w http.ResponseWriter, r *http.Request, p principal) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
		return
	}
	if p.Role.Rank() < auth.RoleOperator.Rank() {
		writeError(w, http.StatusForbidden, fmt.Errorf("insufficient role: requires %s", auth.RoleOperator))
		return
	}
	provider := strings.TrimSpace(r.URL.Query().Get("provider"))
	resp := listProviderModels(provider)
	writeJSON(w, http.StatusOK, resp)
}
