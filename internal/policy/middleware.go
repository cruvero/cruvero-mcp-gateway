package policy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/cruvero/mcp-gateway/internal/identity"
)

const headerPolicyDecision = "X-Policy-Decision"

var (
	policyDeniedObserverMu sync.RWMutex
	policyDeniedObserver   func(reason string, tool string)
)

// SetDeniedObserver sets an optional callback for denied policy decisions.
func SetDeniedObserver(observer func(reason string, tool string)) {
	policyDeniedObserverMu.Lock()
	defer policyDeniedObserverMu.Unlock()
	policyDeniedObserver = observer
}

// PolicyMiddleware evaluates MCP tools/call requests and enforces policy decisions.
func PolicyMiddleware(engine *Engine, logger *slog.Logger) func(http.Handler) http.Handler {
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(os.Stdout, nil))
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if engine == nil || r == nil || r.Body == nil {
				next.ServeHTTP(w, r)
				return
			}

			body, err := io.ReadAll(r.Body)
			r.Body = io.NopCloser(bytes.NewReader(body))
			if err != nil || len(body) == 0 {
				next.ServeHTTP(w, r)
				return
			}

			req, ok := parsePolicyRequest(r, body)
			if !ok {
				next.ServeHTTP(w, r)
				return
			}

			decision, evalErr := engine.Evaluate(r.Context(), req)
			if evalErr != nil {
				writePolicyError(w, http.StatusInternalServerError, "policy evaluation failed", nil)
				return
			}

			if decision.Allowed {
				w.Header().Set(headerPolicyDecision, "allowed")
				next.ServeHTTP(w, r)
				return
			}

			w.Header().Set(headerPolicyDecision, "denied")
			writePolicyError(w, http.StatusForbidden, "policy denied", decision.Violations)
			reason := "policy_denied"
			if len(decision.Violations) > 0 {
				reason = string(decision.Violations[0].Type)
			}
			notifyPolicyDenied(reason, req.ToolName)
			logger.WarnContext(r.Context(), "policy denied request",
				slog.String("tool", req.ToolName),
				slog.String("client_id", req.ClientID),
				slog.Int("violations", len(decision.Violations)),
			)
		})
	}
}

func notifyPolicyDenied(reason string, tool string) {
	policyDeniedObserverMu.RLock()
	observer := policyDeniedObserver
	policyDeniedObserverMu.RUnlock()
	if observer != nil {
		observer(reason, tool)
	}
}

func parsePolicyRequest(r *http.Request, body []byte) (PolicyRequest, bool) {
	var envelope struct {
		Method string `json:"method"`
		Params struct {
			Name        string          `json:"name"`
			Arguments   map[string]any  `json:"arguments"`
			Schema      json.RawMessage `json:"schema"`
			InputSchema json.RawMessage `json:"input_schema"`
		} `json:"params"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return PolicyRequest{}, false
	}
	if strings.TrimSpace(envelope.Method) != "tools/call" {
		return PolicyRequest{}, false
	}

	id, ok := identity.FromContext(r.Context())
	clientID := "anonymous"
	profile := "default"
	if ok {
		if strings.TrimSpace(id.ID) != "" {
			clientID = strings.TrimSpace(id.ID)
		}
		if profileName := strings.TrimSpace(id.Metadata["policy_profile"]); profileName != "" {
			profile = profileName
		}
	}

	schema := envelope.Params.Schema
	if len(schema) == 0 {
		schema = envelope.Params.InputSchema
	}

	return PolicyRequest{
		ToolName:    strings.TrimSpace(envelope.Params.Name),
		Arguments:   envelope.Params.Arguments,
		ClientID:    clientID,
		ProfileName: profile,
		Schema:      schema,
	}, true
}

func writePolicyError(w http.ResponseWriter, status int, message string, violations []Violation) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	payload := map[string]any{
		"error": message,
	}
	if len(violations) > 0 {
		payload["violations"] = violations
	}
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		http.Error(w, fmt.Sprintf("encode response: %v", err), http.StatusInternalServerError)
	}
}
