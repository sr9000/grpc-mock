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
	mux.HandleFunc("/openapi.json", s.handleOpenAPI)
	mux.HandleFunc("/swagger-ui-bundle.js", s.handleSwaggerUIBundle)
	mux.HandleFunc("/swagger-ui.css", s.handleSwaggerUICSS)

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

// handleDoc serves the interactive Swagger UI page
func (s *Server) handleDoc(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(swaggerUIHTML))
}

// handleOpenAPI returns the OpenAPI specification JSON
func (s *Server) handleOpenAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(openapiSpec)
}

// handleSwaggerUIBundle serves the embedded swagger-ui-bundle.js
func (s *Server) handleSwaggerUIBundle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/javascript")
	w.Write(swaggerUIBundleJS)
}

// handleSwaggerUICSS serves the embedded swagger-ui.css
func (s *Server) handleSwaggerUICSS(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "text/css")
	w.Write(swaggerUICSS)
}
