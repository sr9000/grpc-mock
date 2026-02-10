#!/bin/bash

# Test script for gRPC mock management server
# This script validates:
# - Management server endpoints (/doc, /openapi.json, /logs, /clear)
# - gRPC mock server with echo service
# - Recording and clearing of gRPC calls

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Configuration
GRPC_HOST="localhost"
GRPC_PORT="50051"
MGMT_PORT="9000"
MGMT_URL="http://localhost:${MGMT_PORT}"

# Path to the grpc-mock binary
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
BINARY="${PROJECT_DIR}/bin/grpc-mock"

# PID of the server process
SERVER_PID=""

# Cleanup function
cleanup() {
    echo -e "\n${YELLOW}Cleaning up...${NC}"
    if [ -n "$SERVER_PID" ] && kill -0 "$SERVER_PID" 2>/dev/null; then
        kill "$SERVER_PID" 2>/dev/null || true
        wait "$SERVER_PID" 2>/dev/null || true
    fi
    echo -e "${GREEN}Cleanup complete${NC}"
}

# Set trap for cleanup on exit
trap cleanup EXIT

# Helper function to print test results
print_result() {
    local test_name="$1"
    local result="$2"
    if [ "$result" -eq 0 ]; then
        echo -e "${GREEN}✓ PASS${NC}: $test_name"
        return 0
    else
        echo -e "${RED}✗ FAIL${NC}: $test_name"
        return 1
    fi
}

# Helper function to wait for server to be ready
wait_for_server() {
    local url="$1"
    local max_attempts=30
    local attempt=1

    echo -e "${YELLOW}Waiting for server at ${url}...${NC}"
    while [ $attempt -le $max_attempts ]; do
        if curl -s -o /dev/null -w "%{http_code}" "$url" | grep -q "200"; then
            echo -e "${GREEN}Server is ready${NC}"
            return 0
        fi
        sleep 0.5
        attempt=$((attempt + 1))
    done

    echo -e "${RED}Server failed to start${NC}"
    return 1
}

# Build the binary if needed
build_binary() {
    echo -e "${YELLOW}Building grpc-mock binary...${NC}"
    cd "$PROJECT_DIR"
    go build -o bin/grpc-mock ./cmd/grpc-mock
    echo -e "${GREEN}Build complete${NC}"
}

# Start the gRPC mock server
start_server() {
    echo -e "${YELLOW}Starting gRPC mock server...${NC}"
    "$BINARY" run --host "$GRPC_HOST" --port "$GRPC_PORT" --mgmt-port "$MGMT_PORT" --reflection &
    SERVER_PID=$!

    # Wait for both servers to be ready
    wait_for_server "${MGMT_URL}/openapi.json"
}

# Test 1: Check OpenAPI spec endpoint
test_openapi_endpoint() {
    echo -e "\n${YELLOW}Test: OpenAPI endpoint (/openapi.json)${NC}"

    local response
    response=$(curl -s "${MGMT_URL}/openapi.json")

    # Check if it's valid JSON with expected fields
    if echo "$response" | jq -e '.openapi' > /dev/null 2>&1 && \
       echo "$response" | jq -e '.info.title' > /dev/null 2>&1 && \
       echo "$response" | jq -e '.paths["/logs"]' > /dev/null 2>&1; then
        print_result "OpenAPI spec is valid and contains expected fields" 0
    else
        print_result "OpenAPI spec validation" 1
        echo "Response: $response"
        return 1
    fi
}

# Test 2: Check Swagger UI endpoint
test_swagger_ui_endpoint() {
    echo -e "\n${YELLOW}Test: Swagger UI endpoint (/doc)${NC}"

    local response
    local http_code
    response=$(curl -s "${MGMT_URL}/doc")
    http_code=$(curl -s -o /dev/null -w "%{http_code}" "${MGMT_URL}/doc")

    # Check if it returns HTML with Swagger UI elements
    if [ "$http_code" = "200" ] && \
       echo "$response" | grep -q "swagger-ui" && \
       echo "$response" | grep -q "SwaggerUIBundle"; then
        print_result "Swagger UI page is served correctly" 0
    else
        print_result "Swagger UI page validation" 1
        echo "HTTP Code: $http_code"
        return 1
    fi
}

# Test 3: Check Swagger UI assets
test_swagger_ui_assets() {
    echo -e "\n${YELLOW}Test: Swagger UI assets${NC}"

    local js_code
    local css_code
    js_code=$(curl -s -o /dev/null -w "%{http_code}" "${MGMT_URL}/swagger-ui-bundle.js")
    css_code=$(curl -s -o /dev/null -w "%{http_code}" "${MGMT_URL}/swagger-ui.css")

    local result=0
    if [ "$js_code" = "200" ]; then
        print_result "swagger-ui-bundle.js is served (HTTP $js_code)" 0
    else
        print_result "swagger-ui-bundle.js serving" 1
        result=1
    fi

    if [ "$css_code" = "200" ]; then
        print_result "swagger-ui.css is served (HTTP $css_code)" 0
    else
        print_result "swagger-ui.css serving" 1
        result=1
    fi

    return $result
}

# Test 4: Check logs endpoint (should be empty initially)
test_logs_empty() {
    echo -e "\n${YELLOW}Test: Logs endpoint (empty)${NC}"

    local response
    response=$(curl -s "${MGMT_URL}/logs")

    if [ "$response" = "[]" ]; then
        print_result "Logs are empty initially" 0
    else
        print_result "Initial logs check" 1
        echo "Response: $response"
        return 1
    fi
}

# Test 5: Make a gRPC call and check it's recorded
test_grpc_call_recording() {
    echo -e "\n${YELLOW}Test: gRPC call recording${NC}"

    # Make a gRPC call using grpcurl
    if ! command -v grpcurl &> /dev/null; then
        echo -e "${YELLOW}grpcurl not found, skipping gRPC call test${NC}"
        return 0
    fi

    # Call the Echo service
    local grpc_response
    grpc_response=$(grpcurl -plaintext -d '{"message": "Hello, World!"}' \
        "${GRPC_HOST}:${GRPC_PORT}" EchoService/Echo 2>&1) || true

    echo "gRPC Response: $grpc_response"

    # Check if the call was recorded
    local logs
    logs=$(curl -s "${MGMT_URL}/logs")

    if echo "$logs" | jq -e '.[0].method' > /dev/null 2>&1; then
        local method
        local request_message
        method=$(echo "$logs" | jq -r '.[0].method')
        request_message=$(echo "$logs" | jq -r '.[0].request.message // empty')

        if [ "$method" = "/EchoService/Echo" ]; then
            print_result "gRPC call recorded with correct method: $method" 0
        else
            print_result "gRPC call method check" 1
            echo "Expected: /EchoService/Echo, Got: $method"
            return 1
        fi

        if [ "$request_message" = "Hello, World!" ]; then
            print_result "gRPC call recorded with correct request body" 0
        else
            print_result "gRPC call request body check" 1
            echo "Expected message: 'Hello, World!', Got: '$request_message'"
            return 1
        fi

        # Check response is recorded
        local response_message
        response_message=$(echo "$logs" | jq -r '.[0].response.message // empty')
        if [ "$response_message" = "Hello, World!" ]; then
            print_result "gRPC response body recorded correctly" 0
        else
            print_result "gRPC response body check" 1
            echo "Expected response message: 'Hello, World!', Got: '$response_message'"
            return 1
        fi

        # Check timestamp exists
        if echo "$logs" | jq -e '.[0].timestamp' > /dev/null 2>&1; then
            print_result "Timestamp recorded" 0
        else
            print_result "Timestamp check" 1
            return 1
        fi

        # Check duration exists
        if echo "$logs" | jq -e '.[0].duration_ms' > /dev/null 2>&1; then
            print_result "Duration recorded" 0
        else
            print_result "Duration check" 1
            return 1
        fi
    else
        print_result "gRPC call recording" 1
        echo "Logs: $logs"
        return 1
    fi
}

# Test 6: Clear logs
test_clear_logs() {
    echo -e "\n${YELLOW}Test: Clear logs endpoint${NC}"

    # Clear using POST
    local clear_response
    clear_response=$(curl -s -X POST "${MGMT_URL}/clear")

    if echo "$clear_response" | jq -e '.status == "cleared"' > /dev/null 2>&1; then
        print_result "Clear endpoint returns correct response" 0
    else
        print_result "Clear endpoint response" 1
        echo "Response: $clear_response"
        return 1
    fi

    # Verify logs are empty
    local logs
    logs=$(curl -s "${MGMT_URL}/logs")

    if [ "$logs" = "[]" ]; then
        print_result "Logs are empty after clear" 0
    else
        print_result "Logs empty check after clear" 1
        echo "Logs: $logs"
        return 1
    fi
}

# Test 7: Clear logs using DELETE method
test_clear_logs_delete() {
    echo -e "\n${YELLOW}Test: Clear logs with DELETE method${NC}"

    # First make a gRPC call to have something to clear
    if command -v grpcurl &> /dev/null; then
        grpcurl -plaintext -d '{"message": "Test"}' \
            "${GRPC_HOST}:${GRPC_PORT}" EchoService/Echo > /dev/null 2>&1 || true
    fi

    # Clear using DELETE
    local clear_response
    clear_response=$(curl -s -X DELETE "${MGMT_URL}/clear")

    if echo "$clear_response" | jq -e '.status == "cleared"' > /dev/null 2>&1; then
        print_result "Clear with DELETE returns correct response" 0
    else
        print_result "Clear with DELETE response" 1
        echo "Response: $clear_response"
        return 1
    fi

    # Verify logs are empty
    local logs
    logs=$(curl -s "${MGMT_URL}/logs")

    if [ "$logs" = "[]" ]; then
        print_result "Logs are empty after DELETE clear" 0
    else
        print_result "Logs empty check after DELETE clear" 1
        echo "Logs: $logs"
        return 1
    fi
}

# Test 8: Multiple gRPC calls recording
test_multiple_calls() {
    echo -e "\n${YELLOW}Test: Multiple gRPC calls recording${NC}"

    if ! command -v grpcurl &> /dev/null; then
        echo -e "${YELLOW}grpcurl not found, skipping multiple calls test${NC}"
        return 0
    fi

    # Clear first
    curl -s -X POST "${MGMT_URL}/clear" > /dev/null

    # Make multiple calls
    for i in 1 2 3; do
        grpcurl -plaintext -d "{\"message\": \"Message $i\"}" \
            "${GRPC_HOST}:${GRPC_PORT}" EchoService/Echo > /dev/null 2>&1 || true
    done

    # Check all calls are recorded
    local logs
    logs=$(curl -s "${MGMT_URL}/logs")
    local count
    count=$(echo "$logs" | jq 'length')

    if [ "$count" -eq 3 ]; then
        print_result "All 3 gRPC calls recorded" 0
    else
        print_result "Multiple calls recording" 1
        echo "Expected 3 calls, got: $count"
        return 1
    fi

    # Verify each call has unique request_id
    local unique_ids
    unique_ids=$(echo "$logs" | jq '[.[].request_id] | unique | length')

    if [ "$unique_ids" -eq 3 ]; then
        print_result "Each call has unique request_id" 0
    else
        print_result "Unique request_id check" 1
        return 1
    fi
}

# Test 9: Check method not allowed
test_method_not_allowed() {
    echo -e "\n${YELLOW}Test: Method not allowed responses${NC}"

    local result=0

    # POST to /logs should fail
    local code
    code=$(curl -s -o /dev/null -w "%{http_code}" -X POST "${MGMT_URL}/logs")
    if [ "$code" = "405" ]; then
        print_result "POST /logs returns 405" 0
    else
        print_result "POST /logs method check" 1
        result=1
    fi

    # GET to /clear should fail
    code=$(curl -s -o /dev/null -w "%{http_code}" -X GET "${MGMT_URL}/clear")
    if [ "$code" = "405" ]; then
        print_result "GET /clear returns 405" 0
    else
        print_result "GET /clear method check" 1
        result=1
    fi

    return $result
}

# Main test runner
main() {
    echo -e "${YELLOW}========================================${NC}"
    echo -e "${YELLOW}  gRPC Mock Management Server Tests${NC}"
    echo -e "${YELLOW}========================================${NC}"

    # Build and start server
    build_binary
    start_server

    # Run tests
    local failed=0

    test_openapi_endpoint || failed=$((failed + 1))
    test_swagger_ui_endpoint || failed=$((failed + 1))
    test_swagger_ui_assets || failed=$((failed + 1))
    test_logs_empty || failed=$((failed + 1))
    test_grpc_call_recording || failed=$((failed + 1))
    test_clear_logs || failed=$((failed + 1))
    test_clear_logs_delete || failed=$((failed + 1))
    test_multiple_calls || failed=$((failed + 1))
    test_method_not_allowed || failed=$((failed + 1))

    # Summary
    echo -e "\n${YELLOW}========================================${NC}"
    if [ $failed -eq 0 ]; then
        echo -e "${GREEN}All tests passed!${NC}"
    else
        echo -e "${RED}$failed test(s) failed${NC}"
    fi
    echo -e "${YELLOW}========================================${NC}"

    return $failed
}

# Run main
main "$@"
