#!/bin/sh
set -e

# Build and run the server.
# For development: edit code, then restart manually (Ctrl+C + re-run).
echo "Building..."
make build
echo "Starting server..."
exec ./bin/grpc-mock run 0.0.0.0 50051 --reflection
