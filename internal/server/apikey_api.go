package server

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/cruvero/mcp-gateway/internal/auth"
	"github.com/cruvero/mcp-gateway/internal/store"
	"github.com/cruvero/mcp-gateway/internal/types"
	"github.com/go-chi/chi/v5"
)

// APIKeyAPIHandler serves the /v1/apikeys REST endpoints for remote API key management.
type APIKeyAPIHandler struct {
	store  store.APIKeyStore
	logger *slog.Logger
}

// NewAPIKeyAPIHandler creates an API key handler with the given store and logger.
func NewAPIKeyAPIHandler(s store.APIKeyStore, logger *slog.Logger) *APIKeyAPIHandler {
	return &APIKeyAPIHandler{store: s, logger: logger}
}

// Routes returns a chi.Router with create, list, and revoke endpoints.
func (h *APIKeyAPIHandler) Routes() http.Handler {
	r := chi.NewRouter()
	r.Post("/", h.handleCreate)
	r.Get("/", h.handleList)
	r.Delete("/{id}", h.handleRevoke)
	return r
}

type createAPIKeyAPIRequest struct {
	Name      string   `json:"name"`
	Scopes    []string `json:"scopes"`
	Expires   string   `json:"expires"`
	ClientID  string   `json:"client_id"`
	Profile   string   `json:"profile"`
}

type createAPIKeyAPIResponse struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	ClientID      string   `json:"client_id"`
	Scopes        []string `json:"scopes"`
	PolicyProfile string   `json:"profile"`
	ExpiresAt     *string  `json:"expires_at"`
	CreatedAt     string   `json:"created_at"`
	APIKey        string   `json:"api_key"`
}

type apiKeyView struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	ClientID      string   `json:"client_id"`
	Scopes        []string `json:"scopes"`
	PolicyProfile string   `json:"profile"`
	ExpiresAt     *string  `json:"expires_at"`
	CreatedAt     string   `json:"created_at"`
}

func (h *APIKeyAPIHandler) handleCreate(w http.ResponseWriter, r *http.Request) {
	var req createAPIKeyAPIRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}

	scopes := dedupeScopes(req.Scopes)
	if len(scopes) == 0 {
		scopes = []string{"read"}
	}

	var expiresAt *time.Time
	if trimmed := strings.TrimSpace(req.Expires); trimmed != "" {
		t, err := parseAPIKeyExpiry(trimmed)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		expiresAt = t
	}

	plaintext, lookupHash, bcryptHash, err := auth.GenerateAPIKey()
	if err != nil {
		h.logger.Error("generate api key failed", slog.String("error", err.Error()))
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to generate api key"})
		return
	}

	clientID := strings.TrimSpace(req.ClientID)
	if clientID == "" {
		clientID = name
	}
	profile := strings.TrimSpace(req.Profile)
	if profile == "" {
		profile = "default"
	}

	record := &types.APIKey{
		Name:          name,
		ClientID:      clientID,
		Scopes:        scopes,
		PolicyProfile: profile,
		ExpiresAt:     expiresAt,
		KeyLookupHash: lookupHash,
		KeyBcryptHash: bcryptHash,
	}

	if err := h.store.Create(r.Context(), record); err != nil {
		h.logger.Error("create api key failed", slog.String("error", err.Error()))
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create api key"})
		return
	}

	stored, err := h.store.GetByLookupHash(r.Context(), lookupHash)
	if err == nil && stored != nil {
		record.ID = stored.ID
		record.CreatedAt = stored.CreatedAt
	}

	resp := createAPIKeyAPIResponse{
		ID:            record.ID,
		Name:          record.Name,
		ClientID:      record.ClientID,
		Scopes:        record.Scopes,
		PolicyProfile: record.PolicyProfile,
		ExpiresAt:     formatTimePtr(record.ExpiresAt),
		CreatedAt:     record.CreatedAt.UTC().Format(time.RFC3339),
		APIKey:        plaintext,
	}

	writeJSON(w, http.StatusCreated, resp)
}

func (h *APIKeyAPIHandler) handleList(w http.ResponseWriter, r *http.Request) {
	keys, err := h.store.List(r.Context())
	if err != nil {
		h.logger.Error("list api keys failed", slog.String("error", err.Error()))
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list api keys"})
		return
	}

	views := make([]apiKeyView, 0, len(keys))
	for _, key := range keys {
		views = append(views, apiKeyView{
			ID:            key.ID,
			Name:          key.Name,
			ClientID:      key.ClientID,
			Scopes:        key.Scopes,
			PolicyProfile: key.PolicyProfile,
			ExpiresAt:     formatTimePtr(key.ExpiresAt),
			CreatedAt:     key.CreatedAt.UTC().Format(time.RFC3339),
		})
	}

	writeJSON(w, http.StatusOK, views)
}

func (h *APIKeyAPIHandler) handleRevoke(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id is required"})
		return
	}

	if err := h.store.Revoke(r.Context(), id); err != nil {
		h.logger.Error("revoke api key failed", slog.String("id", id), slog.String("error", err.Error()))
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to revoke api key"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "revoked", "id": id})
}

func dedupeScopes(raw []string) []string {
	seen := make(map[string]struct{}, len(raw))
	result := make([]string, 0, len(raw))
	for _, s := range raw {
		trimmed := strings.TrimSpace(s)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}
	return result
}

func formatTimePtr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.UTC().Format(time.RFC3339)
	return &s
}

func parseAPIKeyExpiry(raw string) (*time.Time, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, nil
	}

	var duration time.Duration
	if number, ok := strings.CutSuffix(trimmed, "d"); ok {
		days, err := strconv.Atoi(strings.TrimSpace(number))
		if err != nil || days <= 0 {
			return nil, fmt.Errorf("invalid day duration %q", raw)
		}
		duration = time.Duration(days) * 24 * time.Hour
	} else {
		var err error
		duration, err = time.ParseDuration(trimmed)
		if err != nil {
			return nil, fmt.Errorf("invalid duration %q: %w", raw, err)
		}
	}

	if duration <= 0 {
		return nil, fmt.Errorf("duration must be positive")
	}

	t := time.Now().UTC().Add(duration)
	return &t, nil
}
