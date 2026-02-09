# ==============================================================================
# STAGE 1: Tools & Base Dependencies
# ==============================================================================
FROM golang:1.25-alpine AS tools

# 1. Install System Dependencies
#    - python3: for scripts/update_go_packages.py
#    - protobuf: the 'protoc' compiler
#    - make: to run the pipeline
#    - git: for go mod download
#    - build-base: for gcc (if needed by cgo)
RUN apk add --no-cache bash python3 make git build-base protobuf protobuf-dev

# 2. Install Go Global Tools
#    - protoc plugins for generation
#    - wire for dependency injection
#    - air for hot-reloading (watcher)
RUN go install google.golang.org/protobuf/cmd/protoc-gen-go@latest && \
    go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest && \
    go install github.com/google/wire/cmd/wire@latest && \
    go install github.com/air-verse/air@latest

WORKDIR /app

# 3. Pre-build upd-stubs (Optimization)
#    We copy only what is needed to build the CLI tool first.
#    This allows us to cache the tool compilation even if business logic changes.
COPY go.mod go.sum ./
RUN go mod download

COPY cmd/upd-stubs ./cmd/upd-stubs

# Build and install upd-stubs to global path so it is available in PATH
RUN go build -o /usr/local/bin/upd-stubs ./cmd/upd-stubs

# ==============================================================================
# STAGE 2: Development (Watcher / Hot-Reload)
# ==============================================================================
FROM tools AS dev

WORKDIR /app

# --------------------------------------------------------
# FIX: Trust the mounted directory to allow Git operations
# --------------------------------------------------------
RUN git config --global --add safe.directory /app

# Copy repo to provide template structure
COPY . .

# Start the script
CMD ["./scripts/run-dev.sh"]

# ==============================================================================
# STAGE 3: Production Builder
# ==============================================================================
FROM tools AS builder

WORKDIR /app

# For production build, we copy all source code into the image
COPY . .

# Run the full generation and build pipeline
RUN make all

# ==============================================================================
# STAGE 4: Production Runner (Minimal Image)
# ==============================================================================
FROM alpine:latest AS production

# Install certificates for HTTPS and timezone data
RUN apk add --no-cache ca-certificates tzdata

WORKDIR /home/app

# Create a non-root user for security
RUN addgroup -S appgroup && adduser -S appuser -G appgroup
USER appuser

# Copy only the compiled binary from the builder stage
COPY --from=builder /app/bin/grpc-mock .

# Default configuration
ENV PORT=50051
EXPOSE 50051

ENTRYPOINT ["./grpc-mock"]
CMD ["run"]
