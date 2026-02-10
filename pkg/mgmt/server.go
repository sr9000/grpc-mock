package mgmt

import (
	"context"
	_ "embed"
	"encoding/json"
	"log"
	"net/http"

	"grpc-mock/pkg/recorder"
)

//go:embed openapi.json
var openapiSpec []byte

// Server is the management HTTP server for e2e testing
type Server struct {
	recorder *recorder.Recorder
	server   *http.Server
	port     string
}

// New creates a new management server
func New(rec *recorder.Recorder, port string) *Server {
	return &Server{
		recorder: rec,
		port:     port,
	}
}

// Start starts the management HTTP server
func (s *Server) Start() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/logs", s.handleLogs)
	mux.HandleFunc("/clear", s.handleClear)
	mux.HandleFunc("/doc", s.handleDoc)

	s.server = &http.Server{
		Addr:    ":" + s.port,
		Handler: mux,
	}

	log.Printf("starting management server on port %s", s.port)

	go func() {
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("management server error: %v", err)
		}
	}()

	return nil
}

// Stop gracefully stops the management server
func (s *Server) Stop(ctx context.Context) error {
	if s.server != nil {
		return s.server.Shutdown(ctx)
	}
	return nil
}

// handleLogs returns all recorded gRPC calls as JSON
func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	data, err := s.recorder.ToJSON()
	if err != nil {
		http.Error(w, "failed to serialize logs: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Write(data)
}

// handleClear removes all recorded gRPC calls
func (s *Server) handleClear(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	s.recorder.Clear()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "cleared"})
}

// handleDoc returns the OpenAPI specification
func (s *Server) handleDoc(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(openapiSpec)
}
