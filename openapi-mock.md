# Step-by-Step Plan: OpenAPI Mock Server Tool

A comprehensive guide to building an OpenAPI mock server similar to [grpc-mock](.), adapted for REST/HTTP APIs.

---

## 📋 Overview

| Component | gRPC Mock | OpenAPI Mock |
|-----------|-----------|--------------|
| Schema | `.proto` files | `openapi.yaml/json` |
| Server | gRPC (google.golang.org/grpc) | HTTP (net/http or chi/echo/gin) |
| Code Gen | protoc + protoc-gen-go | openapi-generator / oapi-codegen |
| Stubs | Go interfaces implementing services | Go handlers for endpoints |

---

## Phase 1: Project Scaffolding

### Step 1.1: Initialize Go Module
```bash
mkdir openapi-mock && cd openapi-mock
go mod init openapi-mock
```

### Step 1.2: Create Directory Structure
```
openapi-mock/
├── Makefile
├── Dockerfile
├── docker-compose.yaml
├── docker-compose-grafana.yaml
├── prometheus.yaml
├── cmd/
│   ├── openapi-mock/          # Main server entry point
│   │   └── main.go
│   └── upd-stubs/             # Stub generator CLI
│       └── main.go
├── internal/
│   ├── app/                   # Wire DI
│   │   ├── wire.go
│   │   └── wire_gen.go
│   ├── generated/             # oapi-codegen output (DO NOT EDIT)
│   │   └── petstore/
│   │       ├── types.gen.go
│   │       ├── server.gen.go
│   │       └── spec.gen.go
│   └── stubs/                 # Handler implementations (EDIT HERE)
│       └── petstore/
│           ├── pets.go        # PetsHandlers (endpoints tagged "pets")
│           ├── users.go       # UsersHandlers (endpoints tagged "users")
│           ├── orders.go      # OrdersHandlers (endpoints tagged "orders")
│           └── provider.go    # CompositeHandlers (auto-generated, wires all)
├── pkg/
│   ├── ctxkeys/               # Context keys (request ID)
│   ├── metrics/               # Prometheus metrics server
│   ├── mgmt/                  # Management API (logs, clear)
│   └── recorder/              # Request/response recording
├── specs/                     # <-- Put OpenAPI specs here
│   └── petstore/
│       └── openapi.yaml
├── scripts/
│   ├── gen-openapi.sh         # OpenAPI code generation script
│   └── run-dev.sh             # Development hot-reload script
└── grafana/
    └── provisioning/
        ├── dashboards/
        └── datasources/
```

**Stub Organization Principle**: Each OpenAPI `tag` maps to a separate stub file. This mirrors how grpc-mock separates stubs per gRPC service, keeping related endpoint handlers together for maintainability.

---

## Phase 2: CLI with Cobra + Env

### Step 2.1: Install Dependencies
```bash
go get github.com/spf13/cobra
go get github.com/caarlos0/env/v11
```

### Step 2.2: Create `cmd/openapi-mock/main.go`

```go
package main

import (
    "context"
    "fmt"
    "log"
    "net/http"
    "os"
    "os/signal"
    "syscall"
    "time"

    "github.com/caarlos0/env/v11"
    "github.com/spf13/cobra"
    
    "openapi-mock/internal/app"
    "openapi-mock/pkg/metrics"
    "openapi-mock/pkg/mgmt"
    "openapi-mock/pkg/recorder"
)

type Config struct {
    Host          string `env:"HOST" envDefault:"0.0.0.0"`
    Port          string `env:"PORT" envDefault:"8080"`
    MgmtPort      string `env:"MGMT_PORT" envDefault:"9000"`
    MetricsPort   string `env:"METRICS_PORT" envDefault:"9100"`
    EnableMgmt    bool   `env:"MGMT_ENABLED" envDefault:"true"`
    EnableMetrics bool   `env:"METRICS_ENABLED" envDefault:"true"`
    EnableLogging bool   `env:"HTTP_LOGGING" envDefault:"true"`
}

var version = "dev"

func main() {
    rootCmd := &cobra.Command{
        Use:   "openapi-mock",
        Short: "OpenAPI mock server",
    }

    runCmd := &cobra.Command{
        Use:   "run",
        Short: "Run the HTTP mock server",
        RunE:  runServer,
    }

    // Add flags similar to grpc-mock
    runCmd.Flags().StringP("host", "", "", "Host (overrides HOST env)")
    runCmd.Flags().StringP("port", "p", "", "Port (overrides PORT env)")
    runCmd.Flags().StringP("mgmt-port", "m", "", "Management port")
    runCmd.Flags().StringP("metrics-port", "", "", "Metrics port")
    runCmd.Flags().Bool("no-mgmt", false, "Disable management server")
    runCmd.Flags().Bool("no-metrics", false, "Disable metrics server")
    runCmd.Flags().Bool("no-logs", false, "Disable request logging")

    versionCmd := &cobra.Command{
        Use:   "version",
        Short: "Print version",
        Run: func(cmd *cobra.Command, args []string) {
            fmt.Println(version)
        },
    }

    rootCmd.AddCommand(runCmd, versionCmd)

    if err := rootCmd.Execute(); err != nil {
        os.Exit(1)
    }
}

func runServer(cmd *cobra.Command, args []string) error {
    var cfg Config
    if err := env.Parse(&cfg); err != nil {
        return err
    }
    // Override with flags (same pattern as grpc-mock)
    // ... flag parsing logic ...

    rec := recorder.New()
    
    var metricsServer *metrics.Metrics
    if cfg.EnableMetrics {
        metricsServer = metrics.New(cfg.MetricsPort)
    }

    // Initialize router with all handlers
    router := app.InitializeRouter(rec, metricsServer, cfg.EnableLogging)

    // Start management server
    var mgmtServer *mgmt.Server
    if cfg.EnableMgmt {
        mgmtServer = mgmt.New(rec, cfg.MgmtPort)
        mgmtServer.Start()
    }

    // Start metrics server
    if metricsServer != nil {
        metricsServer.Start()
    }

    // HTTP server
    server := &http.Server{
        Addr:    cfg.Host + ":" + cfg.Port,
        Handler: router,
    }

    // Graceful shutdown
    ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
    defer stop()

    go server.ListenAndServe()
    log.Printf("HTTP server listening on %s:%s", cfg.Host, cfg.Port)

    <-ctx.Done()
    
    shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    server.Shutdown(shutdownCtx)
    
    return nil
}
```

---

## Phase 3: OpenAPI Code Generation

### Step 3.1: Install oapi-codegen
```bash
go install github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@latest
```

### Step 3.2: Create Generation Script `scripts/gen-openapi.sh`

```bash
#!/bin/bash
set -e

SPECS_DIR="specs"
OUTPUT_DIR="internal/generated"

# Find all OpenAPI specs (supports nested directories)
find "$SPECS_DIR" -name "*.yaml" -o -name "*.json" | while read spec; do
    # Skip hidden directories
    if [[ "$spec" == *"/."* ]]; then
        continue
    fi
    
    # Calculate output package name from path
    rel_path="${spec#$SPECS_DIR/}"
    dir_name=$(dirname "$rel_path")
    pkg_name=$(basename "$dir_name" | tr '-' '_')
    
    output_pkg="$OUTPUT_DIR/$dir_name"
    mkdir -p "$output_pkg"
    
    echo "Generating code for: $spec -> $output_pkg"
    
    # Generate types
    oapi-codegen -package "$pkg_name" \
        -generate types \
        -o "$output_pkg/types.gen.go" \
        "$spec"
    
    # Generate server interface (chi router)
    oapi-codegen -package "$pkg_name" \
        -generate chi-server \
        -o "$output_pkg/server.gen.go" \
        "$spec"
    
    # Generate spec embed
    oapi-codegen -package "$pkg_name" \
        -generate spec \
        -o "$output_pkg/spec.gen.go" \
        "$spec"
done

echo "OpenAPI generation complete!"
```

### Step 3.3: Generated Code Structure

For a spec at `specs/petstore/openapi.yaml`, this creates:
```
internal/generated/petstore/
├── types.gen.go    # Request/Response structs
├── server.gen.go   # ServerInterface + chi handler registration
└── spec.gen.go     # Embedded OpenAPI spec
```

---

## Phase 4: Stub Generation & Patching Tool

### Step 4.1: Stub Separation Strategy

**Key Design Decision**: Stubs are separated **per tag (endpoint group)**, similar to how grpc-mock separates stubs per gRPC service.

In OpenAPI, endpoints are grouped using the `tags` field:
```yaml
paths:
  /pets:
    get:
      tags: [pets]        # → internal/stubs/petstore/pets.go
      operationId: GetPets
  /pets/{id}:
    get:
      tags: [pets]        # → internal/stubs/petstore/pets.go
      operationId: GetPetById
  /users:
    get:
      tags: [users]       # → internal/stubs/petstore/users.go
      operationId: GetUsers
  /orders:
    post:
      tags: [orders]      # → internal/stubs/petstore/orders.go
      operationId: CreateOrder
```

**Resulting stub structure:**
```
internal/stubs/petstore/
├── pets.go      # PetsHandlers: GetPets, GetPetById, CreatePet, DeletePet
├── users.go     # UsersHandlers: GetUsers, GetUserById, CreateUser
├── orders.go    # OrdersHandlers: CreateOrder, GetOrder, ListOrders
└── provider.go  # Wire provider that composes all handlers
```

### Step 4.2: Create `cmd/upd-stubs/main.go`

The stub generator should:
1. Parse OpenAPI spec to extract **tags** and their associated operations
2. Scan `internal/generated/` for `ServerInterface` definitions
3. Create **one stub file per tag** in `internal/stubs/<spec>/`
4. Create a `provider.go` that composes all tag handlers into `ServerInterface`
5. **Preserve existing handler logic** on re-generation (AST-based merge)

```go
package main

import (
    "go/ast"
    "go/parser"
    "go/token"
    "os"
    "path/filepath"
    "strings"
    "text/template"
    
    "github.com/getkin/kin-openapi/openapi3"
)

// TagStub represents handlers for one tag group
type TagStub struct {
    Package   string
    TagName   string       // e.g., "pets"
    TypeName  string       // e.g., "PetsHandlers"
    Methods   []MethodInfo
}

// Stub template for individual tag file
const tagStubTemplate = `package {{.Package}}

import (
    "encoding/json"
    "net/http"
    
    gen "openapi-mock/internal/generated/{{.Package}}"
)

// {{.TypeName}} handles {{.TagName}} endpoints
type {{.TypeName}} struct {
    // Add dependencies here (e.g., logger, config)
}

func New{{.TypeName}}() *{{.TypeName}} {
    return &{{.TypeName}}{}
}

{{range .Methods}}
// {{.Name}} - {{.Summary}}
// {{.HTTPMethod}} {{.Path}}
func (h *{{$.TypeName}}) {{.Name}}(w http.ResponseWriter, r *http.Request{{.Params}}) {
{{.Body}}
}
{{end}}
`

// Provider template that composes all handlers
const providerTemplate = `package {{.Package}}

import (
    gen "openapi-mock/internal/generated/{{.Package}}"
)

// CompositeHandlers implements gen.ServerInterface by delegating to tag-specific handlers
type CompositeHandlers struct {
{{range .Tags}}
    {{.FieldName}} *{{.TypeName}}
{{end}}
}

// NewCompositeHandlers creates the composite handler
func NewCompositeHandlers(
{{range .Tags}}
    {{.ParamName}} *{{.TypeName}},
{{end}}
) gen.ServerInterface {
    return &CompositeHandlers{
{{range .Tags}}
        {{.FieldName}}: {{.ParamName}},
{{end}}
    }
}

{{range .DelegationMethods}}
func (c *CompositeHandlers) {{.Name}}({{.Params}}) {
    c.{{.DelegateTo}}.{{.Name}}({{.Args}})
}
{{end}}
`

func main() {
    // 1. Load OpenAPI spec and extract tags → operations mapping
    // 2. Scan internal/generated for ServerInterface method signatures
    // 3. For each tag:
    //    a. Parse existing stub file (if exists) to extract method bodies
    //    b. Merge: keep existing bodies, add new methods
    //    c. Write updated stub file
    // 4. Generate/update provider.go with composite handler
    
    // Implementation similar to grpc-mock/cmd/upd-stubs
    // Uses go/ast to parse and preserve existing code
}

// extractTagsFromSpec parses OpenAPI and returns tag → []operation mapping
func extractTagsFromSpec(specPath string) (map[string][]string, error) {
    loader := openapi3.NewLoader()
    doc, err := loader.LoadFromFile(specPath)
    if err != nil {
        return nil, err
    }
    
    tags := make(map[string][]string)
    for path, pathItem := range doc.Paths.Map() {
        for method, op := range pathItem.Operations() {
            if op.OperationID == "" {
                continue
            }
            for _, tag := range op.Tags {
                tags[tag] = append(tags[tag], op.OperationID)
            }
            // Operations without tags go to "default" group
            if len(op.Tags) == 0 {
                tags["default"] = append(tags["default"], op.OperationID)
            }
        }
    }
    return tags, nil
}
```

### Step 4.3: Key Features of Stub Generator

| Feature | Description |
|---------|-------------|
| **Tag-based Separation** | One file per OpenAPI tag (endpoint group) |
| **Interface Discovery** | Finds `ServerInterface` in generated packages |
| **AST Preservation** | Parses existing stubs, keeps method bodies intact |
| **Composite Provider** | Generates `provider.go` to compose all tag handlers |
| **Default Response** | New methods return `501 Not Implemented` or example from spec |
| **Import Management** | Auto-adds required imports |

### Step 4.4: Example Generated Stubs

**pets.go** - Handlers for `pets` tag:
```go
// internal/stubs/petstore/pets.go
package petstore

import (
    "encoding/json"
    "net/http"
    
    gen "openapi-mock/internal/generated/petstore"
)

// PetsHandlers handles pets endpoints
type PetsHandlers struct{}

func NewPetsHandlers() *PetsHandlers {
    return &PetsHandlers{}
}

// GetPets - List all pets
// GET /pets
func (h *PetsHandlers) GetPets(w http.ResponseWriter, r *http.Request, params gen.GetPetsParams) {
    // TODO: Implement your mock logic here
    pets := []gen.Pet{
        {Id: 1, Name: "Fluffy", Tag: ptr("cat")},
        {Id: 2, Name: "Buddy", Tag: ptr("dog")},
    }
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(pets)
}

// CreatePet - Create a pet
// POST /pets
func (h *PetsHandlers) CreatePet(w http.ResponseWriter, r *http.Request) {
    // Your custom logic here - PRESERVED on regeneration
    var pet gen.Pet
    json.NewDecoder(r.Body).Decode(&pet)
    pet.Id = 123 // mock ID
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(http.StatusCreated)
    json.NewEncoder(w).Encode(pet)
}

// GetPetById - Get pet by ID
// GET /pets/{petId}
func (h *PetsHandlers) GetPetById(w http.ResponseWriter, r *http.Request, petId int64) {
    pet := gen.Pet{Id: petId, Name: "Mock Pet"}
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(pet)
}
```

**users.go** - Handlers for `users` tag:
```go
// internal/stubs/petstore/users.go
package petstore

import (
    "encoding/json"
    "net/http"
    
    gen "openapi-mock/internal/generated/petstore"
)

// UsersHandlers handles users endpoints
type UsersHandlers struct{}

func NewUsersHandlers() *UsersHandlers {
    return &UsersHandlers{}
}

// GetUsers - List all users
// GET /users
func (h *UsersHandlers) GetUsers(w http.ResponseWriter, r *http.Request) {
    users := []gen.User{
        {Id: 1, Username: "john"},
        {Id: 2, Username: "jane"},
    }
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(users)
}
```

**provider.go** - Composite handler (auto-generated):
```go
// internal/stubs/petstore/provider.go
package petstore

import (
    gen "openapi-mock/internal/generated/petstore"
)

// CompositeHandlers implements gen.ServerInterface by delegating to tag-specific handlers
type CompositeHandlers struct {
    pets   *PetsHandlers
    users  *UsersHandlers
    orders *OrdersHandlers
}

// NewCompositeHandlers creates the composite handler
func NewCompositeHandlers(
    pets *PetsHandlers,
    users *UsersHandlers,
    orders *OrdersHandlers,
) gen.ServerInterface {
    return &CompositeHandlers{
        pets:   pets,
        users:  users,
        orders: orders,
    }
}

// Delegation methods (auto-generated)
func (c *CompositeHandlers) GetPets(w http.ResponseWriter, r *http.Request, params gen.GetPetsParams) {
    c.pets.GetPets(w, r, params)
}

func (c *CompositeHandlers) CreatePet(w http.ResponseWriter, r *http.Request) {
    c.pets.CreatePet(w, r)
}

func (c *CompositeHandlers) GetPetById(w http.ResponseWriter, r *http.Request, petId int64) {
    c.pets.GetPetById(w, r, petId)
}

func (c *CompositeHandlers) GetUsers(w http.ResponseWriter, r *http.Request) {
    c.users.GetUsers(w, r)
}

// ... more delegation methods
```

---

## Phase 5: Wire Dependency Injection

### Step 5.1: Create `internal/app/wire.go`

Wire configuration wires all tag handlers through the `CompositeHandlers` provider:

```go
//go:build wireinject
// +build wireinject

package app

import (
    "net/http"
    
    "github.com/go-chi/chi/v5"
    "github.com/google/wire"
    
    "openapi-mock/pkg/metrics"
    "openapi-mock/pkg/recorder"
    
    // Generated packages
    petstore_gen "openapi-mock/internal/generated/petstore"
    
    // Stub implementations (tag-based handlers + composite)
    petstore_stubs "openapi-mock/internal/stubs/petstore"
)

func InitializeRouter(rec *recorder.Recorder, m *metrics.Metrics, enableLogging bool) http.Handler {
    wire.Build(
        // Tag-specific handler providers
        petstore_stubs.NewPetsHandlers,
        petstore_stubs.NewUsersHandlers,
        petstore_stubs.NewOrdersHandlers,
        
        // Composite provider that implements ServerInterface
        petstore_stubs.NewCompositeHandlers,
        
        // Router assembly
        NewRouter,
    )
    return nil
}

func NewRouter(
    petstoreHandlers petstore_gen.ServerInterface,
    // Add more API handlers as specs grow
) http.Handler {
    r := chi.NewRouter()
    
    // Register all API handlers
    petstore_gen.HandlerFromMux(petstoreHandlers, r)
    
    return r
}
```

### Step 5.2: Auto-Update wire.go

The `upd-stubs` tool should also update `wire.go` to:
- Add imports for new generated/stub packages
- Register **each tag handler** provider (e.g., `NewPetsHandlers`, `NewUsersHandlers`)
- Register the composite provider (`NewCompositeHandlers`)

### Step 5.3: Wire Dependency Graph

```
                    ┌─────────────────────┐
                    │  InitializeRouter   │
                    └──────────┬──────────┘
                               │
                    ┌──────────▼──────────┐
                    │      NewRouter      │
                    └──────────┬──────────┘
                               │
              ┌────────────────┼────────────────┐
              │                │                │
    ┌─────────▼─────────┐     ...     ┌────────▼────────┐
    │ petstore.Server   │             │ another.Server  │
    │    Interface      │             │    Interface    │
    └─────────┬─────────┘             └─────────────────┘
              │
    ┌─────────▼─────────┐
    │CompositeHandlers  │
    └─────────┬─────────┘
              │
    ┌─────────┼─────────┬─────────────┐
    │         │         │             │
┌───▼───┐ ┌───▼───┐ ┌───▼───┐   ┌─────▼─────┐
│ Pets  │ │ Users │ │Orders │   │  Default  │
│Handler│ │Handler│ │Handler│   │  Handler  │
└───────┘ └───────┘ └───────┘   └───────────┘
```

---

## Phase 6: Recording Middleware & Management API

### Step 6.1: Create Recording Middleware

```go
// pkg/middleware/recording.go
package middleware

import (
    "bytes"
    "io"
    "net/http"
    "time"
    
    "openapi-mock/pkg/recorder"
    "openapi-mock/pkg/metrics"
)

func RecordingMiddleware(rec *recorder.Recorder, m *metrics.Metrics, enableLogging bool) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            reqID := generateRequestID()
            start := time.Now()
            
            // Capture request body
            bodyBytes, _ := io.ReadAll(r.Body)
            r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
            
            // Wrap response writer to capture status/body
            rw := &responseWriter{ResponseWriter: w, statusCode: 200}
            
            // Add request ID to context
            ctx := context.WithValue(r.Context(), ctxkeys.RequestID{}, reqID)
            r = r.WithContext(ctx)
            
            // Handle panics
            defer func() {
                if err := recover(); err != nil {
                    duration := time.Since(start)
                    rec.Record(recorder.CallRecord{
                        RequestID:  reqID,
                        Method:     r.Method + " " + r.URL.Path,
                        Timestamp:  start,
                        Request:    string(bodyBytes),
                        Panic:      fmt.Sprintf("%v", err),
                        DurationMs: duration.Milliseconds(),
                    })
                    if m != nil {
                        m.RecordRequest(r.URL.Path, duration.Milliseconds(), "panic")
                        m.RecordPanic(r.URL.Path, fmt.Sprintf("%v", err))
                    }
                    http.Error(w, "Internal Server Error", 500)
                }
            }()
            
            next.ServeHTTP(rw, r)
            
            duration := time.Since(start)
            status := statusToCategory(rw.statusCode)
            
            rec.Record(recorder.CallRecord{
                RequestID:  reqID,
                Method:     r.Method + " " + r.URL.Path,
                Timestamp:  start,
                Request:    string(bodyBytes),
                Response:   rw.body.String(),
                DurationMs: duration.Milliseconds(),
            })
            
            if m != nil {
                m.RecordRequest(r.URL.Path, duration.Milliseconds(), status)
                if rw.statusCode >= 400 {
                    m.RecordError(r.URL.Path, http.StatusText(rw.statusCode))
                }
            }
        })
    }
}
```

### Step 6.2: Management Server (reuse from grpc-mock)

The `pkg/mgmt` package can be reused almost entirely:
- `GET /logs` - return recorded calls
- `POST /clear` - clear records
- `GET /doc` - Swagger UI
- `GET /openapi.json` - OpenAPI spec

---

## Phase 7: Prometheus Metrics

### Step 7.1: Adapt Metrics Package

```go
// pkg/metrics/metrics.go
package metrics

import (
    "github.com/prometheus/client_golang/prometheus"
    "github.com/prometheus/client_golang/prometheus/promhttp"
)

type Metrics struct {
    RequestsTotal   *prometheus.CounterVec   // labels: path, method, status
    RequestDuration *prometheus.HistogramVec // labels: path, method, status
    ErrorsTotal     *prometheus.CounterVec   // labels: path, error
    PanicsTotal     *prometheus.CounterVec   // labels: path, panic
    
    // Resource metrics (same as grpc-mock)
    MemoryUsage *prometheus.GaugeVec
    Goroutines  prometheus.Gauge
    
    registry *prometheus.Registry
    server   *http.Server
    port     string
}

func New(port string) *Metrics {
    registry := prometheus.NewRegistry()
    
    m := &Metrics{
        RequestsTotal: prometheus.NewCounterVec(
            prometheus.CounterOpts{
                Name: "http_requests_total",
                Help: "Total HTTP requests",
            },
            []string{"path", "method", "status"},
        ),
        RequestDuration: prometheus.NewHistogramVec(
            prometheus.HistogramOpts{
                Name:    "http_request_duration_seconds",
                Help:    "HTTP request latency histogram",
                Buckets: prometheus.DefBuckets,
            },
            []string{"path", "method", "status"},
        ),
        // ... rest similar to grpc-mock
    }
    
    // Register metrics
    registry.MustRegister(m.RequestsTotal, m.RequestDuration, ...)
    registry.MustRegister(collectors.NewGoCollector())
    registry.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
    
    return m
}
```

### Step 7.2: Metrics Summary

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `http_requests_total` | Counter | path, method, status | Total requests |
| `http_request_duration_seconds` | Histogram | path, method, status | Latency distribution |
| `http_errors_total` | Counter | path, error | Error count by message |
| `http_panics_total` | Counter | path, panic | Panic count by message |

---

## Phase 8: Grafana Dashboards

### Step 8.1: Create Dashboard JSON

Adapt the grpc-mock dashboards for HTTP metrics:

**Overview Dashboard** (`grafana/provisioning/dashboards/overview.json`):
- Total RPS: `sum(rate(http_requests_total[1m]))`
- Error Rate: `sum(rate(http_requests_total{status="error"}[1m]))`
- P50/P95/P99 Latency: `histogram_quantile(0.99, rate(http_request_duration_seconds_bucket[5m]))`

**Endpoint Details Dashboard** (`grafana/provisioning/dashboards/endpoint-details.json`):
- Variables: `$endpoint` (path), `$method` (GET/POST/etc)
- RPS by status (stacked)
- Status distribution (% stacked)
- Error breakdown
- Latency heatmap

### Step 8.2: Dashboard Variables

```json
{
  "templating": {
    "list": [
      {
        "name": "endpoint",
        "query": "label_values(http_requests_total, path)",
        "type": "query"
      },
      {
        "name": "method", 
        "query": "label_values(http_requests_total{path=\"$endpoint\"}, method)",
        "type": "query"
      }
    ]
  }
}
```

---

## Phase 9: Docker Setup

### Step 9.1: Dockerfile (Multi-stage)

```dockerfile
# Stage 1: Tools
FROM golang:1.25-alpine AS tools

RUN apk add --no-cache bash make git
RUN go install github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@latest && \
    go install github.com/google/wire/cmd/wire@latest && \
    go install github.com/air-verse/air@latest

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY cmd/upd-stubs ./cmd/upd-stubs
RUN go build -o /usr/local/bin/upd-stubs ./cmd/upd-stubs

# Stage 2: Development
FROM tools AS dev
RUN git config --global --add safe.directory /app
COPY . .
CMD ["./scripts/run-dev.sh"]

# Stage 3: Builder
FROM tools AS builder
COPY . .
RUN make all

# Stage 4: Production
FROM alpine:latest AS production
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /home/app
RUN addgroup -S appgroup && adduser -S appuser -G appgroup
USER appuser
COPY --from=builder /app/bin/openapi-mock .
ENV PORT=8080
EXPOSE 8080
ENTRYPOINT ["./openapi-mock"]
CMD ["run"]
```

### Step 9.2: docker-compose.yaml (Dev)

```yaml
services:
  openapi-mock:
    build:
      context: .
      target: dev
    volumes:
      - .:/app
      - go-cache:/go/pkg/mod
    ports:
      - "8080:8080"
      - "9000:9000"
      - "9100:9100"
    environment:
      - HTTP_LOGGING=true

volumes:
  go-cache:
```

### Step 9.3: docker-compose-grafana.yaml (Full Stack)

```yaml
services:
  openapi-mock:
    build:
      context: .
      target: production
    ports:
      - "8080:8080"
      - "9000:9000"
      - "9100:9100"
    environment:
      - HOST=0.0.0.0

  prometheus:
    image: prom/prometheus:latest
    volumes:
      - ./prometheus.yaml:/etc/prometheus/prometheus.yaml:ro
    ports:
      - "9090:9090"
    depends_on:
      - openapi-mock

  grafana:
    build:
      context: ./grafana
    environment:
      - PROMETHEUS_URL=http://prometheus:9090
    ports:
      - "3000:3000"
    depends_on:
      - prometheus

volumes:
  prometheus_data:
```

---

## Phase 10: Makefile Automation

### Step 10.1: Complete Makefile

```makefile
.PHONY: all build run proto stub wire clean help

.DEFAULT_GOAL := help

all: proto stub wire build

# Generate OpenAPI code
proto:
	@echo "===================="
	@echo "Generating OpenAPI code..."
	./scripts/gen-openapi.sh

# Update/create stubs
stub:
	@echo "===================="
	@echo "Updating stubs..."
	@if command -v upd-stubs >/dev/null 2>&1; then \
		upd-stubs; \
	else \
		go build -o bin/upd-stubs ./cmd/upd-stubs && ./bin/upd-stubs; \
	fi

# Generate Wire DI
wire:
	@echo "===================="
	@echo "Generating Wire..."
	go run github.com/google/wire/cmd/wire@latest gen ./internal/app

# Build binary
build:
	@echo "===================="
	@echo "Building server..."
	go build -o bin/openapi-mock ./cmd/openapi-mock

# Run locally
run:
	@echo "===================="
	@echo "Running server..."
	go run ./cmd/openapi-mock run

# Docker targets
docker-build:
	docker build -t openapi-mock:latest .

docker-run:
	docker run --rm -p 8080:8080 -p 9000:9000 -p 9100:9100 openapi-mock:latest

docker-dev:
	docker compose up --build

# Full stack with monitoring
compose-up:
	docker compose -f docker-compose-grafana.yaml up --build -d

compose-logs:
	docker compose -f docker-compose-grafana.yaml logs -f

compose-down:
	docker compose -f docker-compose-grafana.yaml down

# Cleanup
clean:
	rm -rf bin/
	rm -rf internal/generated/

# Help
help:
	@echo "Available targets:"
	@echo "  all          - Full pipeline (proto + stub + wire + build)"
	@echo "  proto        - Generate code from OpenAPI specs"
	@echo "  stub         - Update handler stubs"
	@echo "  wire         - Generate DI wiring"
	@echo "  build        - Build binary"
	@echo "  run          - Run server locally"
	@echo "  docker-build - Build production image"
	@echo "  docker-run   - Run production container"
	@echo "  docker-dev   - Start dev environment (hot reload)"
	@echo "  compose-up   - Start full stack with monitoring"
	@echo "  compose-logs - Follow stack logs"
	@echo "  compose-down - Stop full stack"
	@echo "  clean        - Remove generated files"
```

---

## 📊 Summary: Comparison Table

| Aspect | gRPC Mock | OpenAPI Mock |
|--------|-----------|--------------|
| **Schema Location** | `protos/` | `specs/` |
| **Code Generator** | `protoc` + plugins | `oapi-codegen` |
| **Generated Output** | `internal/genproto/` | `internal/generated/` |
| **Stub Location** | `internal/stubs/` | `internal/stubs/` |
| **Stub Separation** | Per gRPC service | Per OpenAPI tag (endpoint group) |
| **Stub Files** | `<service>.go` | `<tag>.go` + `provider.go` |
| **Server Framework** | google.golang.org/grpc | chi (or net/http) |
| **Interceptor** | grpc.UnaryInterceptor | HTTP middleware |
| **Default Port** | 50051 | 8080 |
| **Protocol** | gRPC/HTTP2 | REST/HTTP |
| **Metrics Prefix** | `grpc_*` | `http_*` |

---

## 🚀 Quick Start Commands

```bash
# 1. Add your OpenAPI spec
cp your-api.yaml specs/myapi/openapi.yaml

# 2. Generate everything and build
make all

# 3. Edit stubs (one file per tag/endpoint group)
vim internal/stubs/myapi/pets.go    # Edit "pets" tag handlers
vim internal/stubs/myapi/users.go   # Edit "users" tag handlers
# Note: provider.go is auto-generated, don't edit manually

# 4. Run locally
make run

# 5. Run full stack with monitoring
make compose-up
# Open http://localhost:3000 for Grafana
# Open http://localhost:9000/doc for Swagger UI
```
