.PHONY: all build run gen-proto gen-wire clean help

# Default target: show help when `make` is called without arguments
help:

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
	go run github.com/google/wire/cmd/wire@latest gen ./internal/app

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
	go run ./cmd/grpc-mock

# Show help
help:
	@echo "Available targets:"
	@echo "  proto  - (Re)Generate .pb.go files"
	@echo "  stub   - Update gRPC stubs"
	@echo "  wire   - Update wire dependency injection"
	@echo "  build  - Build the server binary"
	@echo "  run    - Run the server"
