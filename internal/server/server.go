package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"sync/atomic"
	"time"

	"github.com/cruvero/mcp-gateway/internal/config"
	"github.com/cruvero/mcp-gateway/internal/identity"
	"github.com/go-chi/chi/v5"
)

const shutdownTimeout = 10 * time.Second

// Server wraps the gateway HTTP server and router.
type Server struct {
	cfg        *config.Config
	logger     *slog.Logger
	router     chi.Router
	httpServer *http.Server
	startTime  time.Time
	ready      atomic.Bool
}

// New builds a configured HTTP server with middleware and routes.
func New(cfg *config.Config, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(os.Stdout, nil))
	}

	router := chi.NewRouter()
	router.Use(RequestIDMiddleware)
	router.Use(LoggingMiddleware(logger))
	router.Use(RecoveryMiddleware(logger))
	router.Use(RequestBodyLimitMiddleware(defaultMaxRequestBodyBytes))
	if cfg != nil && cfg.CORSEnabled {
		router.Use(CORSMiddleware)
	}

	srv := &Server{
		cfg:       cfg,
		logger:    logger,
		router:    router,
		startTime: time.Now().UTC(),
	}
	srv.ready.Store(true)
	srv.setupRoutes()

	listenAddr := ":8443"
	if cfg != nil && cfg.ListenAddr != "" {
		listenAddr = cfg.ListenAddr
	}

	srv.httpServer = &http.Server{
		Addr:              listenAddr,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
	}

	return srv
}

// Start starts the HTTP server and shuts it down gracefully when the context is cancelled.
func (s *Server) Start(ctx context.Context) error {
	if s == nil {
		return fmt.Errorf("start server: server is nil")
	}
	if ctx == nil {
		return fmt.Errorf("start server: context is nil")
	}

	shutdownErrCh := make(chan error, 1)
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		shutdownErrCh <- s.httpServer.Shutdown(shutdownCtx)
	}()

	var serveErr error
	if s.cfg != nil && s.cfg.IsTLSConfigured() {
		serveErr = s.httpServer.ListenAndServeTLS(s.cfg.TLSCertPath, s.cfg.TLSKeyPath)
	} else {
		serveErr = s.httpServer.ListenAndServe()
	}

	if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
		return fmt.Errorf("start server: %w", serveErr)
	}

	select {
	case shutdownErr := <-shutdownErrCh:
		if shutdownErr != nil {
			return fmt.Errorf("start server: shutdown: %w", shutdownErr)
		}
	default:
	}

	return nil
}

// MountRegistrationRoutes mounts registration routes under /v1/registrations with mTLS identity middleware.
func (s *Server) MountRegistrationRoutes(registrationHandler http.Handler) {
	if s == nil || registrationHandler == nil {
		return
	}

	allowedPrefixes := []string{}
	if s.cfg != nil {
		allowedPrefixes = s.cfg.SPIFFEAllowList
	}

	s.router.Route("/v1/registrations", func(r chi.Router) {
		r.Use(identity.MTLSMiddleware(allowedPrefixes, s.logger))
		r.Mount("/", registrationHandler)
	})
}

// Handler returns the root HTTP handler for testing and embedding.
func (s *Server) Handler() http.Handler {
	if s == nil {
		return http.NotFoundHandler()
	}
	return s.router
}

func (s *Server) setupRoutes() {
	s.router.Get("/healthz", s.handleHealth)
	s.router.Get("/readyz", s.handleReady)
	s.router.Get("/metrics", s.handleMetrics)
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleReady(w http.ResponseWriter, _ *http.Request) {
	if !s.ready.Load() {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "not_ready"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (s *Server) handleMetrics(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "metrics_placeholder"})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
	}
}
