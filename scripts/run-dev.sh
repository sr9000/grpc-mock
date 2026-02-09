#!/bin/sh
set -e

# 1. Start Proto Watcher in the background.
#    It handles generation (proto -> stubs).
air -c .air.proto.toml &

# 2. Start Go Watcher in the foreground.
#    It handles compilation (go -> binary) and restarts the server.
#    We pass the runtime arguments here (host, port, reflection).
sleep 5
exec air -c .air.go.toml -- run 0.0.0.0 50051 --reflection
