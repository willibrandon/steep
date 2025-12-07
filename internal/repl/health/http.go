// Package health provides HTTP health endpoints for load balancer integration.
package health

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"sync"
	"time"
)

// ServerConfig holds HTTP server configuration.
type ServerConfig struct {
	Port int
	Bind string // Bind address, e.g., "0.0.0.0" or "127.0.0.1"
}

// ComponentHealth represents the health status of a single component.
type ComponentHealth struct {
	Status  string `json:"status"` // "healthy", "unhealthy", "degraded"
	Message string `json:"message,omitempty"`
}

// HealthResponse is the JSON response for /health endpoint.
type HealthResponse struct {
	Status     string                     `json:"status"` // "healthy", "unhealthy", "degraded"
	NodeID     string                     `json:"node_id,omitempty"`
	NodeName   string                     `json:"node_name,omitempty"`
	Version    string                     `json:"version,omitempty"`
	Uptime     string                     `json:"uptime,omitempty"`
	Components map[string]ComponentHealth `json:"components,omitempty"`
}

// ReadyResponse is the JSON response for /ready endpoint.
type ReadyResponse struct {
	Ready  bool   `json:"ready"`
	Reason string `json:"reason,omitempty"`
}

// LiveResponse is the JSON response for /live endpoint.
type LiveResponse struct {
	Alive bool `json:"alive"`
}

// HealthProvider is an interface for retrieving daemon health status.
type HealthProvider interface {
	GetNodeID() string
	GetNodeName() string
	GetVersion() string
	GetUptime() time.Duration
	IsPostgreSQLConnected() bool
	GetPostgreSQLStatus() string
	IsGRPCRunning() bool
	GetGRPCPort() int
	IsIPCRunning() bool
}

// Server is the HTTP health endpoint server.
type Server struct {
	config   ServerConfig
	provider HealthProvider
	logger   *log.Logger
	debug    bool

	server   *http.Server
	listener net.Listener

	mu      sync.Mutex
	running bool
}

// NewServer creates a new HTTP health server.
func NewServer(config ServerConfig, provider HealthProvider, logger *log.Logger, debug bool) *Server {
	if config.Bind == "" {
		config.Bind = "0.0.0.0"
	}
	if config.Port == 0 {
		config.Port = 8080
	}

	return &Server{
		config:   config,
		provider: provider,
		logger:   logger,
		debug:    debug,
	}
}

// Start starts the HTTP server.
func (s *Server) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return fmt.Errorf("server already running")
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/ready", s.handleReady)
	mux.HandleFunc("/live", s.handleLive)
	mux.HandleFunc("/livez", s.handleLive)   // Kubernetes alias
	mux.HandleFunc("/readyz", s.handleReady) // Kubernetes alias

	addr := fmt.Sprintf("%s:%d", s.config.Bind, s.config.Port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", addr, err)
	}
	s.listener = listener

	s.server = &http.Server{
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	s.running = true
	s.logger.Printf("HTTP health server listening on %s", addr)

	go func() {
		if err := s.server.Serve(listener); err != nil && err != http.ErrServerClosed {
			s.logger.Printf("HTTP server error: %v", err)
		}
	}()

	return nil
}

// Stop gracefully shuts down the HTTP server.
func (s *Server) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := s.server.Shutdown(ctx); err != nil {
		return fmt.Errorf("shutdown error: %w", err)
	}

	s.running = false
	s.logger.Println("HTTP health server stopped")
	return nil
}

// Port returns the server port.
func (s *Server) Port() int {
	return s.config.Port
}

// handleHealth handles the /health endpoint.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	components := s.aggregateComponentHealth()
	overallStatus := s.calculateOverallStatus(components)

	s.logRequest(r, "/health", overallStatus)

	resp := HealthResponse{
		Status:     overallStatus,
		NodeID:     s.provider.GetNodeID(),
		NodeName:   s.provider.GetNodeName(),
		Version:    s.provider.GetVersion(),
		Uptime:     formatDuration(s.provider.GetUptime()),
		Components: components,
	}

	statusCode := http.StatusOK
	switch overallStatus {
	case "unhealthy":
		statusCode = http.StatusServiceUnavailable
	case "degraded":
		statusCode = http.StatusOK // Still return 200 for degraded
	}

	s.writeJSON(w, statusCode, resp)
}

// handleReady handles the /ready endpoint.
func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Ready when PostgreSQL is connected
	pgConnected := s.provider.IsPostgreSQLConnected()

	status := "ready"
	if !pgConnected {
		status = "not_ready"
	}
	s.logRequest(r, "/ready", status)

	resp := ReadyResponse{
		Ready: pgConnected,
	}

	if !pgConnected {
		resp.Reason = "PostgreSQL not connected"
	}

	statusCode := http.StatusOK
	if !pgConnected {
		statusCode = http.StatusServiceUnavailable
	}

	s.writeJSON(w, statusCode, resp)
}

// handleLive handles the /live endpoint.
func (s *Server) handleLive(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	s.logRequest(r, "/live", "alive")

	// Always alive if we can respond
	resp := LiveResponse{
		Alive: true,
	}

	s.writeJSON(w, http.StatusOK, resp)
}

// aggregateComponentHealth collects health status from all components.
func (s *Server) aggregateComponentHealth() map[string]ComponentHealth {
	components := make(map[string]ComponentHealth)

	// PostgreSQL
	if s.provider.IsPostgreSQLConnected() {
		components["postgresql"] = ComponentHealth{
			Status:  "healthy",
			Message: s.provider.GetPostgreSQLStatus(),
		}
	} else {
		components["postgresql"] = ComponentHealth{
			Status:  "unhealthy",
			Message: "disconnected",
		}
	}

	// gRPC
	if s.provider.IsGRPCRunning() {
		components["grpc"] = ComponentHealth{
			Status:  "healthy",
			Message: fmt.Sprintf("listening on port %d", s.provider.GetGRPCPort()),
		}
	} else {
		components["grpc"] = ComponentHealth{
			Status:  "unhealthy",
			Message: "not running",
		}
	}

	// IPC
	if s.provider.IsIPCRunning() {
		components["ipc"] = ComponentHealth{
			Status:  "healthy",
			Message: "listening",
		}
	} else {
		components["ipc"] = ComponentHealth{
			Status:  "degraded",
			Message: "disabled or not running",
		}
	}

	return components
}

// calculateOverallStatus determines the overall health status from components.
func (s *Server) calculateOverallStatus(components map[string]ComponentHealth) string {
	hasUnhealthy := false
	hasDegraded := false

	for _, c := range components {
		switch c.Status {
		case "unhealthy":
			hasUnhealthy = true
		case "degraded":
			hasDegraded = true
		}
	}

	// PostgreSQL is critical - if it's down, we're unhealthy
	if pg, ok := components["postgresql"]; ok && pg.Status == "unhealthy" {
		return "unhealthy"
	}

	if hasUnhealthy {
		return "unhealthy"
	}
	if hasDegraded {
		return "degraded"
	}
	return "healthy"
}

// logRequest logs an incoming HTTP request.
func (s *Server) logRequest(r *http.Request, endpoint, status string) {
	if !s.debug {
		return
	}
	s.logger.Printf("HTTP %s from %s: %s", endpoint, r.RemoteAddr, status)
}

// writeJSON writes a JSON response.
func (s *Server) writeJSON(w http.ResponseWriter, statusCode int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		s.logger.Printf("Error encoding JSON response: %v", err)
	}
}

// formatDuration formats a duration in human-readable form.
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
	}
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	return fmt.Sprintf("%dd%dh", days, hours)
}
