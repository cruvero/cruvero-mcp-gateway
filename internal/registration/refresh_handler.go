package registration

import (
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	identitypkg "github.com/cruvero/mcp-gateway/internal/identity"
	"github.com/cruvero/mcp-gateway/internal/store"
	"github.com/go-chi/chi/v5"
)

const (
	refreshRateLimit    = 10
	refreshRateWindow   = time.Minute
	errMissingServerID  = "missing registration id"
	errSPIFFEMismatch   = "SPIFFE ID mismatch"
	errRefreshRateLimit = "rate limit exceeded"
)

// RefreshHandler handles push-based capability refresh requests.
type RefreshHandler struct {
	service     *Service
	serverStore store.ServerStore
	logger      *slog.Logger

	mu       sync.Mutex
	counters map[string]*rateBucket
}

type rateBucket struct {
	count     int
	windowEnd time.Time
}

// NewRefreshHandler creates a handler for POST /v1/registrations/{id}/capabilities.
func NewRefreshHandler(service *Service, serverStore store.ServerStore, logger *slog.Logger) *RefreshHandler {
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(os.Stdout, nil))
	}
	return &RefreshHandler{
		service:     service,
		serverStore: serverStore,
		logger:      logger,
		counters:    make(map[string]*rateBucket),
	}
}

// ServeHTTP handles the push refresh endpoint.
func (h *RefreshHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if id == "" {
		writeRegistrationError(w, http.StatusBadRequest, errMissingServerID)
		return
	}

	caller, ok := identitypkg.FromContext(r.Context())
	if !ok {
		writeRegistrationError(w, http.StatusUnauthorized, errMissingIdentity)
		return
	}

	record, err := h.serverStore.Get(r.Context(), id)
	if err != nil {
		writeRegistrationError(w, http.StatusNotFound, "server not found")
		return
	}

	if caller.ID != record.SPIFFEID {
		writeRegistrationError(w, http.StatusForbidden, errSPIFFEMismatch)
		return
	}

	if !h.allowRequest(id) {
		writeRegistrationError(w, http.StatusTooManyRequests, errRefreshRateLimit)
		return
	}

	result, err := h.service.RefreshCapabilities(r.Context(), id)
	if err != nil {
		h.logger.ErrorContext(r.Context(), "refresh capabilities failed",
			slog.String("server_id", id),
			slog.String("error", err.Error()),
		)
		writeRegistrationError(w, http.StatusInternalServerError, "refresh failed")
		return
	}

	writeRegistrationJSON(w, http.StatusOK, result)
}

// allowRequest enforces a simple rate limit of refreshRateLimit requests per
// refreshRateWindow per server ID. Returns true if the request is allowed.
func (h *RefreshHandler) allowRequest(serverID string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()

	now := time.Now()
	bucket, exists := h.counters[serverID]
	if !exists || now.After(bucket.windowEnd) {
		h.counters[serverID] = &rateBucket{
			count:     1,
			windowEnd: now.Add(refreshRateWindow),
		}
		return true
	}

	if bucket.count >= refreshRateLimit {
		return false
	}

	bucket.count++
	return true
}
