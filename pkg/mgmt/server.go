package mgmt

import (
	"context"
	_ "embed"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"grpc-mock/pkg/recorder"
)

//go:embed openapi.json
var openapiSpec []byte

//go:embed swagger-ui-bundle.js
var swaggerUIBundleJS []byte

//go:embed swagger-ui.css
var swaggerUICSS []byte

// Swagger UI HTML template that loads embedded assets
const swaggerUIHTML = `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>gRPC Mock Management API</title>
    <link rel="stylesheet" href="/swagger-ui.css">
</head>
<body>
    <div id="swagger-ui"></div>
    <script src="/swagger-ui-bundle.js"></script>
    <script>
        window.onload = function() {
            SwaggerUIBundle({
                url: "/openapi.json",
                dom_id: '#swagger-ui',
                presets: [
                    SwaggerUIBundle.presets.apis,
                    SwaggerUIBundle.SwaggerUIStandalonePreset
                ],
                layout: "BaseLayout"
            });
        };
    </script>
</body>
</html>`

// Server is the management HTTP server for e2e testing
type Server struct {
	recorder *recorder.Recorder
	reset    func(context.Context) error
	server   *http.Server
	port     string
}

// New creates a new management server
func New(rec *recorder.Recorder, port string, opts ...Option) *Server {
	s := &Server{
		recorder: rec,
		port:     port,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Option configures a management server.
type Option func(*Server)

// WithReset sets the reset callback for POST /reset.
func WithReset(fn func(context.Context) error) Option {
	return func(s *Server) {
		s.reset = fn
	}
}

// Start starts the management HTTP server
func (s *Server) Start() error {
	s.server = &http.Server{
		Addr:    ":" + s.port,
		Handler: s.router(),
	}

	log.Printf("starting management server on port %s", s.port)

	go func() {
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("management server error: %v", err)
		}
	}()

	return nil
}

func (s *Server) router() http.Handler {
	r := chi.NewRouter()
	r.Get("/logs", s.handleLogs)
	r.Delete("/logs", s.handleDeleteLogs)
	r.Get("/logs/{request_id}", s.handleLogsByRequestID)

	// Deprecated: use DELETE /logs instead
	r.Post("/clear", s.handleClear)
	r.Delete("/clear", s.handleClear)

	r.Post("/reset", s.handleReset)
	r.Get("/doc", s.handleDoc)
	r.Get("/openapi.json", s.handleOpenAPI)
	r.Get("/swagger-ui-bundle.js", s.handleSwaggerUIBundle)
	r.Get("/swagger-ui.css", s.handleSwaggerUICSS)
	return r
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
	w.Header().Set("Content-Type", "application/json")

	data, err := s.recorder.ToJSON()
	if err != nil {
		http.Error(w, "failed to serialize logs: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Write(data)
}

// handleLogsByRequestID returns all records for a given request_id
func (s *Server) handleLogsByRequestID(w http.ResponseWriter, r *http.Request) {
	requestID := chi.URLParam(r, "request_id")

	data, err := json.Marshal(s.recorder.GetRecordsByRequestID(requestID))
	if err != nil {
		http.Error(w, "failed to serialize logs: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
}

// handleDeleteLogs clears all recorded gRPC calls (Core endpoint)
func (s *Server) handleDeleteLogs(w http.ResponseWriter, r *http.Request) {
	s.recorder.Clear()
	writeJSON(w, http.StatusOK, map[string]string{"status": "cleared"})
}

// handleClear removes all recorded gRPC calls (deprecated: use DELETE /logs)
func (s *Server) handleClear(w http.ResponseWriter, r *http.Request) {
	s.recorder.Clear()
	writeJSON(w, http.StatusOK, map[string]string{"status": "cleared"})
}

// handleReset performs a soft reset (clears records + any state)
func (s *Server) handleReset(w http.ResponseWriter, r *http.Request) {
	s.recorder.Clear()
	if s.reset != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		if err := s.reset(ctx); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "reset failed: " + err.Error()})
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "reset"})
}

// handleDoc serves the interactive Swagger UI page
func (s *Server) handleDoc(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(swaggerUIHTML))
}

// handleOpenAPI returns the OpenAPI specification JSON
func (s *Server) handleOpenAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write(openapiSpec)
}

// handleSwaggerUIBundle serves the embedded swagger-ui-bundle.js
func (s *Server) handleSwaggerUIBundle(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/javascript")
	w.Write(swaggerUIBundleJS)
}

// handleSwaggerUICSS serves the embedded swagger-ui.css
func (s *Server) handleSwaggerUICSS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/css")
	w.Write(swaggerUICSS)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(payload)
}
