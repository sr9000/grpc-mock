.PHONY: all build run gen-proto gen-wire clean help validate-observability compose-up compose-logs compose-down compose-smoke

# Default target: show help when `make` is called without arguments
.DEFAULT_GOAL := help

DEV_COMPOSE_FILE := docker-compose.dev.yaml
OBSERVABILITY_COMPOSE_FILE := docker-compose.observability.yaml
COMPOSE_ENV_PATH := $(or $(wildcard .env),$(wildcard deploy/.env))
COMPOSE_ENV_FILE := $(if $(COMPOSE_ENV_PATH),--env-file $(COMPOSE_ENV_PATH),)

all: proto stub wire build

# Update go_package in proto files
update-proto-pkg:
	@echo
	@echo "===================="
	@echo "Updating go_package in proto files..."
	python3 scripts/update_go_packages.py

# Generate protobuf files
proto: update-proto-pkg
	@echo
	@echo "===================="
	@echo "(Re)Generating protobufs..."
	./scripts/gen-protos.sh

stub:
	@echo
	@echo "===================="
	@echo "Updating stubs..."
	@# Check if upd-stubs exists in PATH (Docker environment)
	@# OR fallback for local run: build it first
	@if command -v upd-stubs >/dev/null 2>&1; then \
		upd-stubs; \
	else \
		go build -o bin/upd-stubs ./cmd/upd-stubs && ./bin/upd-stubs; \
	fi

# Generate wire dependency injection
wire:
	@echo
	@echo "===================="
	@echo "Updating wire..."
	go tool wire gen ./internal/app

# Build the server
build:
	@echo
	@echo "===================="
	@echo "Building server..."
	go build -o bin/grpc-mock ./cmd/grpc-mock

# Run the server
run:
	@echo
	@echo "===================="
	@echo "Running server..."
	go run ./cmd/grpc-mock run

# Docker operations
docker-build:
	@echo
	@echo "===================="
	@echo "Building Docker image..."
	docker build -t grpc-mock:latest .

docker-run:
	@echo
	@echo "===================="
	@echo "Running Docker container..."
	docker run --rm -p 50051:50051 -p 9000:9000 -p 9100:9100 grpc-mock:latest

docker-dev:
	@echo
	@echo "===================="
	@echo "Starting development environment..."
	docker compose $(COMPOSE_ENV_FILE) -f $(DEV_COMPOSE_FILE) up --build

# Docker Compose (Full Observability Stack)
compose-up:
	@echo
	@echo "===================="
	@echo "Starting observability stack (gRPC Mock + Prometheus + Loki + Tempo + OTel + Grafana)..."
	docker compose $(COMPOSE_ENV_FILE) -f $(OBSERVABILITY_COMPOSE_FILE) --progress plain build
	docker compose $(COMPOSE_ENV_FILE) -f $(OBSERVABILITY_COMPOSE_FILE) up -d

compose-logs:
	@echo
	@echo "===================="
	@echo "Following logs..."
	docker compose $(COMPOSE_ENV_FILE) -f $(OBSERVABILITY_COMPOSE_FILE) logs -f

compose-down:
	@echo
	@echo "===================="
	@echo "Stopping observability stack..."
	docker compose $(COMPOSE_ENV_FILE) -f $(OBSERVABILITY_COMPOSE_FILE) down

# Smoke test: verify the full stack works end-to-end
compose-smoke:
	@echo
	@echo "===================="
	@echo "Running observability stack smoke test..."
	./scripts/validate-observability-stack.sh

# Validate the full observability stack (Prometheus + Grafana + metrics + dashboards)
validate-observability:
	@echo
	@echo "===================="
	@echo "Validating observability stack..."
	./scripts/validate-observability-stack.sh

# Show help
help:
	@echo "Available targets:"
	@echo "  proto        - (Re)Generate .pb.go files"
	@echo "  stub         - Update gRPC stubs"
	@echo "  wire         - Update wire dependency injection"
	@echo "  build        - Build the server binary"
	@echo "  run          - Run the server"
	@echo "  docker-build - Build production Docker image"
	@echo "  docker-run   - Run production Docker container"
	@echo "  docker-dev   - Start development environment (build + run via docker-compose.dev.yaml)"
	@echo "  compose-up   - Start observability stack (Mock + Monitoring via docker-compose.observability.yaml)"
	@echo "  compose-logs - Follow logs of observability stack"
	@echo "  compose-down - Stop observability stack"
	@echo "  compose-smoke - Smoke test the observability stack"
	@echo "  validate-observability - Validate full observability stack (Prometheus + Loki + Tempo + Grafana)"
