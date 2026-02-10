package api

import (
	"fmt"
	"net/http"

	"github.com/PipeOpsHQ/agent-sdk-go/guardrail"
)

// handleGuardrails handles GET /api/v1/guardrails — returns the catalog of built-in guardrails.
func (s *Server) handleGuardrails(w http.ResponseWriter, r *http.Request, _ principal) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
		return
	}
	catalog := guardrail.BuiltinCatalog()
	writeJSON(w, http.StatusOK, map[string]any{"guardrails": catalog, "count": len(catalog)})
}
