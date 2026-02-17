package registration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/cruvero/mcp-gateway/internal/auth"
	identitypkg "github.com/cruvero/mcp-gateway/internal/identity"
	"github.com/cruvero/mcp-gateway/internal/types"
	"github.com/go-chi/chi/v5"
)

// RegistrationService defines service methods used by registration HTTP handlers.
type RegistrationService interface {
	Register(ctx context.Context, caller *identitypkg.Identity, req RegistrationRequest) (*RegistrationResponse, error)
	Heartbeat(ctx context.Context, caller *identitypkg.Identity, id string, req HeartbeatRequest) (*HeartbeatResponse, error)
	List(ctx context.Context, filter types.ServerFilter) ([]types.ServerRecord, error)
	Deregister(ctx context.Context, caller *identitypkg.Identity, id string) error
}

// Handler provides HTTP endpoints for registration operations.
type Handler struct {
	service RegistrationService
	logger  *slog.Logger
}

// NewHandler creates a registration handler.
func NewHandler(service RegistrationService, logger *slog.Logger) *Handler {
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(os.Stdout, nil))
	}
	return &Handler{service: service, logger: logger}
}

// Routes returns the registration API router.
func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Post("/", h.handleRegister)
	r.Post("/{id}/heartbeat", h.handleHeartbeat)
	r.With(auth.RequireScope(identitypkg.ScopeAdmin)).Get("/", h.handleList)
	r.Delete("/{id}", h.handleDeregister)
	return r
}

func (h *Handler) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req RegistrationRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeRegistrationError(w, http.StatusBadRequest, err.Error())
		return
	}

	caller, ok := identitypkg.FromContext(r.Context())
	if !ok {
		writeRegistrationError(w, http.StatusUnauthorized, "missing identity")
		return
	}

	resp, err := h.service.Register(r.Context(), caller, req)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	writeRegistrationJSON(w, http.StatusCreated, resp)
}

func (h *Handler) handleList(w http.ResponseWriter, r *http.Request) {
	filter, err := parseServerFilter(r)
	if err != nil {
		writeRegistrationError(w, http.StatusBadRequest, err.Error())
		return
	}

	records, err := h.service.List(r.Context(), filter)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	writeRegistrationJSON(w, http.StatusOK, records)
}

func (h *Handler) handleHeartbeat(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if id == "" {
		writeRegistrationError(w, http.StatusBadRequest, "missing registration id")
		return
	}

	caller, ok := identitypkg.FromContext(r.Context())
	if !ok {
		writeRegistrationError(w, http.StatusUnauthorized, "missing identity")
		return
	}

	var req HeartbeatRequest
	if r.Body != nil {
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			writeRegistrationError(w, http.StatusBadRequest, "invalid request body")
			return
		}
	}

	resp, err := h.service.Heartbeat(r.Context(), caller, id, req)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	writeRegistrationJSON(w, http.StatusOK, resp)
}

func (h *Handler) handleDeregister(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if id == "" {
		writeRegistrationError(w, http.StatusBadRequest, "missing registration id")
		return
	}

	caller, ok := identitypkg.FromContext(r.Context())
	if !ok {
		writeRegistrationError(w, http.StatusUnauthorized, "missing identity")
		return
	}

	if err := h.service.Deregister(r.Context(), caller, id); err != nil {
		h.writeServiceError(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) writeServiceError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	message := "internal server error"

	switch {
	case errors.Is(err, ErrInvalidRequest):
		status = http.StatusBadRequest
		message = err.Error()
	case errors.Is(err, ErrUnauthorized):
		status = http.StatusUnauthorized
		message = err.Error()
	case errors.Is(err, ErrForbidden):
		status = http.StatusForbidden
		message = err.Error()
	case errors.Is(err, ErrNotFound):
		status = http.StatusNotFound
		message = err.Error()
	default:
		h.logger.Error("registration handler error", slog.String("error", err.Error()))
	}

	writeRegistrationError(w, status, message)
}

func parseServerFilter(r *http.Request) (types.ServerFilter, error) {
	query := r.URL.Query()
	filter := types.ServerFilter{
		NamePattern: strings.TrimSpace(query.Get("name")),
	}

	if statusValue := strings.TrimSpace(query.Get("status")); statusValue != "" {
		status := types.ServerStatus(statusValue)
		switch status {
		case types.StatusPending, types.StatusApproved, types.StatusActive, types.StatusStale, types.StatusExpired:
			filter.Status = &status
		default:
			return types.ServerFilter{}, fmt.Errorf("invalid status filter")
		}
	}

	if limitValue := strings.TrimSpace(query.Get("limit")); limitValue != "" {
		limit, err := strconv.Atoi(limitValue)
		if err != nil || limit < 0 {
			return types.ServerFilter{}, fmt.Errorf("invalid limit")
		}
		filter.Limit = limit
	}
	if offsetValue := strings.TrimSpace(query.Get("offset")); offsetValue != "" {
		offset, err := strconv.Atoi(offsetValue)
		if err != nil || offset < 0 {
			return types.ServerFilter{}, fmt.Errorf("invalid offset")
		}
		filter.Offset = offset
	}

	return filter, nil
}

func decodeJSONBody(r *http.Request, dst any) error {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return fmt.Errorf("invalid request body")
	}
	return nil
}

func writeRegistrationJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
	}
}

func writeRegistrationError(w http.ResponseWriter, status int, message string) {
	writeRegistrationJSON(w, status, map[string]string{"error": message})
}
