package mgmt

import (
	"context"
	"encoding/json"
	"fmt"
	"grpc-mock/pkg/mm"
	"grpc-mock/pkg/recorder"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
)

func TestHandleLogs(t *testing.T) {
	rec := recorder.New()
	rec.Record(recorder.CallRecord{
		RequestID:  "test-id",
		Method:     "/TestService/TestMethod",
		Timestamp:  time.Now(),
		Request:    map[string]string{"message": "hello"},
		Response:   map[string]string{"message": "world"},
		DurationMs: 50,
	})

	s := New(rec, "9000")

	req := httptest.NewRequest(http.MethodGet, "/logs", nil)
	w := httptest.NewRecorder()

	s.handleLogs(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	if resp.Header.Get("Content-Type") != "application/json" {
		t.Errorf("Expected Content-Type 'application/json', got '%s'", resp.Header.Get("Content-Type"))
	}

	body, _ := io.ReadAll(resp.Body)
	var records []recorder.CallRecord
	if err := json.Unmarshal(body, &records); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if len(records) != 1 {
		t.Fatalf("Expected 1 record, got %d", len(records))
	}

	if records[0].RequestID != "test-id" {
		t.Errorf("Expected request_id 'test-id', got '%s'", records[0].RequestID)
	}
}

func TestHandleLogsEmpty(t *testing.T) {
	rec := recorder.New()
	s := New(rec, "9000")

	req := httptest.NewRequest(http.MethodGet, "/logs", nil)
	w := httptest.NewRecorder()

	s.handleLogs(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if string(body) != "[]" {
		t.Errorf("Expected empty array '[]', got '%s'", string(body))
	}
}

func TestHandleLogsMethodNotAllowed(t *testing.T) {
	rec := recorder.New()
	s := New(rec, "9000")
	r := s.router()

	methods := []string{http.MethodPost, http.MethodPut, http.MethodPatch}

	for _, method := range methods {
		req := httptest.NewRequest(method, "/logs", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		resp := w.Result()
		resp.Body.Close()

		if resp.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("Expected status 405 for %s /logs, got %d", method, resp.StatusCode)
		}
	}
}

func TestHandleClearPost(t *testing.T) {
	rec := recorder.New()
	rec.Record(recorder.CallRecord{
		RequestID: "test-id",
		Method:    "/TestService/TestMethod",
		Timestamp: time.Now(),
	})

	s := New(rec, "9000")

	req := httptest.NewRequest(http.MethodPost, "/clear", nil)
	w := httptest.NewRecorder()

	s.handleClear(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	var result map[string]string
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if result["status"] != "cleared" {
		t.Errorf("Expected status 'cleared', got '%s'", result["status"])
	}

	if len(rec.GetRecords()) != 0 {
		t.Error("Expected records to be cleared")
	}
}

func TestHandleClearDelete(t *testing.T) {
	rec := recorder.New()
	rec.Record(recorder.CallRecord{
		RequestID: "test-id",
		Method:    "/TestService/TestMethod",
		Timestamp: time.Now(),
	})

	s := New(rec, "9000")

	req := httptest.NewRequest(http.MethodDelete, "/clear", nil)
	w := httptest.NewRecorder()

	s.handleClear(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	if len(rec.GetRecords()) != 0 {
		t.Error("Expected records to be cleared")
	}
}

func TestHandleClearMethodNotAllowed(t *testing.T) {
	rec := recorder.New()
	s := New(rec, "9000")
	r := s.router()

	methods := []string{http.MethodGet, http.MethodPut, http.MethodPatch}

	for _, method := range methods {
		req := httptest.NewRequest(method, "/clear", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		resp := w.Result()
		resp.Body.Close()

		if resp.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("Expected status 405 for %s /clear, got %d", method, resp.StatusCode)
		}
	}
}

func TestHandleLogsByRequestID(t *testing.T) {
	rec := recorder.New()
	rec.Record(recorder.CallRecord{
		RequestID: "id-1",
		Method:    "/TestService/MethodA",
		Timestamp: time.Now(),
	})
	rec.Record(recorder.CallRecord{
		RequestID: "id-2",
		Method:    "/TestService/MethodB",
		Timestamp: time.Now(),
	})
	rec.Record(recorder.CallRecord{
		RequestID: "id-1",
		Method:    "/TestService/MethodC",
		Timestamp: time.Now(),
	})

	s := New(rec, "9000")
	r := s.router()

	// Test existing request_id
	req := httptest.NewRequest(http.MethodGet, "/logs/id-1", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	var records []recorder.CallRecord
	if err := json.Unmarshal(body, &records); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if len(records) != 2 {
		t.Fatalf("Expected 2 records for id-1, got %d", len(records))
	}

	// Test nonexistent request_id
	req = httptest.NewRequest(http.MethodGet, "/logs/nonexistent", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	resp = w.Result()
	defer resp.Body.Close()

	body, _ = io.ReadAll(resp.Body)
	if err := json.Unmarshal(body, &records); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if len(records) != 0 {
		t.Fatalf("Expected 0 records for nonexistent id, got %d", len(records))
	}
}

func TestHandleDeleteLogs(t *testing.T) {
	rec := recorder.New()
	rec.Record(recorder.CallRecord{
		RequestID: "test-id",
		Method:    "/TestService/TestMethod",
		Timestamp: time.Now(),
	})

	s := New(rec, "9000")

	req := httptest.NewRequest(http.MethodDelete, "/logs", nil)
	w := httptest.NewRecorder()

	s.handleDeleteLogs(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	var result map[string]string
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if result["status"] != "cleared" {
		t.Errorf("Expected status 'cleared', got '%s'", result["status"])
	}

	if len(rec.GetRecords()) != 0 {
		t.Error("Expected records to be cleared")
	}
}

func TestHandleReset(t *testing.T) {
	rec := recorder.New()
	rec.Record(recorder.CallRecord{
		RequestID: "test-id",
		Method:    "/TestService/TestMethod",
		Timestamp: time.Now(),
	})

	resetCalled := false
	s := New(rec, "9000", WithReset(func(ctx context.Context) error {
		resetCalled = true
		return nil
	}))

	req := httptest.NewRequest(http.MethodPost, "/reset", nil)
	w := httptest.NewRecorder()

	s.handleReset(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	var result map[string]string
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if result["status"] != "reset" {
		t.Errorf("Expected status 'reset', got '%s'", result["status"])
	}

	if len(rec.GetRecords()) != 0 {
		t.Error("Expected records to be cleared after reset")
	}

	if !resetCalled {
		t.Error("Expected reset callback to be called")
	}
}

func TestHandleResetNoCallback(t *testing.T) {
	rec := recorder.New()
	rec.Record(recorder.CallRecord{
		RequestID: "test-id",
		Method:    "/TestService/TestMethod",
		Timestamp: time.Now(),
	})

	s := New(rec, "9000")

	req := httptest.NewRequest(http.MethodPost, "/reset", nil)
	w := httptest.NewRecorder()

	s.handleReset(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	if len(rec.GetRecords()) != 0 {
		t.Error("Expected records to be cleared after reset even without callback")
	}
}

func TestHandleResetCallbackError(t *testing.T) {
	rec := recorder.New()
	rec.Record(recorder.CallRecord{
		RequestID: "test-id",
		Method:    "/TestService/TestMethod",
		Timestamp: time.Now(),
	})

	s := New(rec, "9000", WithReset(func(ctx context.Context) error {
		return fmt.Errorf("reset failed")
	}))

	req := httptest.NewRequest(http.MethodPost, "/reset", nil)
	w := httptest.NewRecorder()

	s.handleReset(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("Expected status 500, got %d", resp.StatusCode)
	}
}

func TestRouterIntegration(t *testing.T) {
	rec := recorder.New()
	s := New(rec, "9000")
	r := s.router()

	// Test DELETE /logs via router
	rec.Record(recorder.CallRecord{
		RequestID: "test-id",
		Method:    "/TestService/TestMethod",
		Timestamp: time.Now(),
	})

	req := httptest.NewRequest(http.MethodDelete, "/logs", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200 for DELETE /logs, got %d", resp.StatusCode)
	}

	if len(rec.GetRecords()) != 0 {
		t.Error("Expected records to be cleared after DELETE /logs")
	}
}

func TestHandleDoc(t *testing.T) {
	rec := recorder.New()
	s := New(rec, "9000")

	req := httptest.NewRequest(http.MethodGet, "/doc", nil)
	w := httptest.NewRecorder()

	s.handleDoc(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	if resp.Header.Get("Content-Type") != "text/html; charset=utf-8" {
		t.Errorf("Expected Content-Type 'text/html; charset=utf-8', got '%s'", resp.Header.Get("Content-Type"))
	}

	body, _ := io.ReadAll(resp.Body)
	bodyStr := string(body)

	if !contains(bodyStr, "swagger-ui") {
		t.Error("Expected response to contain 'swagger-ui'")
	}

	if !contains(bodyStr, "SwaggerUIBundle") {
		t.Error("Expected response to contain 'SwaggerUIBundle'")
	}
}

func TestHandleDocMethodNotAllowed(t *testing.T) {
	rec := recorder.New()
	s := New(rec, "9000")
	r := s.router()

	req := httptest.NewRequest(http.MethodPost, "/doc", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	resp := w.Result()
	resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("Expected status 405, got %d", resp.StatusCode)
	}
}

func TestHandleOpenAPI(t *testing.T) {
	rec := recorder.New()
	s := New(rec, "9000")

	req := httptest.NewRequest(http.MethodGet, "/openapi.json", nil)
	w := httptest.NewRecorder()

	s.handleOpenAPI(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	if resp.Header.Get("Content-Type") != "application/json" {
		t.Errorf("Expected Content-Type 'application/json', got '%s'", resp.Header.Get("Content-Type"))
	}

	body, _ := io.ReadAll(resp.Body)
	var spec map[string]any
	if err := json.Unmarshal(body, &spec); err != nil {
		t.Fatalf("Failed to unmarshal OpenAPI spec: %v", err)
	}

	if spec["openapi"] != "3.0.3" {
		t.Errorf("Expected openapi version '3.0.3', got '%v'", spec["openapi"])
	}

	info, ok := spec["info"].(map[string]any)
	if !ok {
		t.Fatal("Expected 'info' field in spec")
	}

	if info["title"] != "gRPC Mock Management API" {
		t.Errorf("Expected title 'gRPC Mock Management API', got '%v'", info["title"])
	}
}

func TestHandleSwaggerUIBundle(t *testing.T) {
	rec := recorder.New()
	s := New(rec, "9000")

	req := httptest.NewRequest(http.MethodGet, "/swagger-ui-bundle.js", nil)
	w := httptest.NewRecorder()

	s.handleSwaggerUIBundle(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	if resp.Header.Get("Content-Type") != "application/javascript" {
		t.Errorf("Expected Content-Type 'application/javascript', got '%s'", resp.Header.Get("Content-Type"))
	}

	body, _ := io.ReadAll(resp.Body)
	if len(body) < 1000 {
		t.Error("Expected swagger-ui-bundle.js to be a large file")
	}
}

func TestHandleSwaggerUICSS(t *testing.T) {
	rec := recorder.New()
	s := New(rec, "9000")

	req := httptest.NewRequest(http.MethodGet, "/swagger-ui.css", nil)
	w := httptest.NewRecorder()

	s.handleSwaggerUICSS(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	if resp.Header.Get("Content-Type") != "text/css" {
		t.Errorf("Expected Content-Type 'text/css', got '%s'", resp.Header.Get("Content-Type"))
	}

	body, _ := io.ReadAll(resp.Body)
	if len(body) < 1000 {
		t.Error("Expected swagger-ui.css to be a large file")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// --- Context-values tests ---

func TestHandleGetContextValuesEmpty(t *testing.T) {
	rec := recorder.New()
	s := New(rec, "9000")
	r := s.router()

	req := httptest.NewRequest(http.MethodGet, "/context-values", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	var result map[string]map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("Expected empty map, got %v", result)
	}
}

func TestHandlePutContextValues(t *testing.T) {
	rec := recorder.New()
	s := New(rec, "9000")
	r := s.router()

	payload := `{"req-1": {"key1": "val1", "key2": 42}}`
	req := httptest.NewRequest(http.MethodPut, "/context-values", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	// Verify via GET
	req = httptest.NewRequest(http.MethodGet, "/context-values", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	resp = w.Result()
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result map[string]map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}
	if result["req-1"]["key1"] != "val1" {
		t.Errorf("Expected key1=val1, got %v", result["req-1"]["key1"])
	}
}

func TestHandlePatchContextValues(t *testing.T) {
	rec := recorder.New()
	s := New(rec, "9000")
	r := s.router()

	// Seed initial data
	payload := `{"req-1": {"key1": "val1"}}`
	req := httptest.NewRequest(http.MethodPut, "/context-values", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Patch with new key
	payload = `{"req-1": {"key2": "val2"}}`
	req = httptest.NewRequest(http.MethodPatch, "/context-values", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result map[string]map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}
	if result["req-1"]["key1"] != "val1" {
		t.Errorf("Expected key1=val1 after patch, got %v", result["req-1"]["key1"])
	}
	if result["req-1"]["key2"] != "val2" {
		t.Errorf("Expected key2=val2 after patch, got %v", result["req-1"]["key2"])
	}
}

func TestHandleDeleteContextValues(t *testing.T) {
	rec := recorder.New()
	s := New(rec, "9000")
	r := s.router()

	// Seed data
	payload := `{"req-1": {"key1": "val1"}}`
	req := httptest.NewRequest(http.MethodPut, "/context-values", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Delete all
	req = httptest.NewRequest(http.MethodDelete, "/context-values", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	// Verify empty
	req = httptest.NewRequest(http.MethodGet, "/context-values", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	resp = w.Result()
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result map[string]map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("Expected empty map after delete, got %v", result)
	}
}

func TestHandleGetContextValuesByRequestID(t *testing.T) {
	rec := recorder.New()
	s := New(rec, "9000")
	r := s.router()

	// Seed data
	payload := `{"req-1": {"key1": "val1"}, "req-2": {"key2": "val2"}}`
	req := httptest.NewRequest(http.MethodPut, "/context-values", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Get by request_id
	req = httptest.NewRequest(http.MethodGet, "/context-values/req-1", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}
	if result["key1"] != "val1" {
		t.Errorf("Expected key1=val1, got %v", result["key1"])
	}
}

func TestHandlePutContextValuesByRequestID(t *testing.T) {
	rec := recorder.New()
	s := New(rec, "9000")
	r := s.router()

	payload := `{"key1": "val1"}`
	req := httptest.NewRequest(http.MethodPut, "/context-values/req-1", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	// Verify via GET by request_id
	req = httptest.NewRequest(http.MethodGet, "/context-values/req-1", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	resp = w.Result()
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}
	if result["key1"] != "val1" {
		t.Errorf("Expected key1=val1, got %v", result["key1"])
	}
}

func TestHandlePatchContextValuesByRequestID(t *testing.T) {
	rec := recorder.New()
	s := New(rec, "9000")
	r := s.router()

	// Seed
	payload := `{"key1": "val1"}`
	req := httptest.NewRequest(http.MethodPut, "/context-values/req-1", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Patch
	payload = `{"key2": "val2"}`
	req = httptest.NewRequest(http.MethodPatch, "/context-values/req-1", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}
	if result["key1"] != "val1" {
		t.Errorf("Expected key1=val1 after patch, got %v", result["key1"])
	}
	if result["key2"] != "val2" {
		t.Errorf("Expected key2=val2 after patch, got %v", result["key2"])
	}
}

func TestHandleDeleteContextValuesByRequestID(t *testing.T) {
	rec := recorder.New()
	s := New(rec, "9000")
	r := s.router()

	// Seed
	payload := `{"req-1": {"key1": "val1"}, "req-2": {"key2": "val2"}}`
	req := httptest.NewRequest(http.MethodPut, "/context-values", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Delete by request_id (no body = delete entire request_id)
	req = httptest.NewRequest(http.MethodDelete, "/context-values/req-1", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	// Verify req-1 is gone but req-2 remains
	req = httptest.NewRequest(http.MethodGet, "/context-values", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	resp = w.Result()
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result map[string]map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}
	if _, ok := result["req-1"]; ok {
		t.Error("Expected req-1 to be deleted")
	}
	if result["req-2"]["key2"] != "val2" {
		t.Errorf("Expected req-2 key2=val2, got %v", result["req-2"]["key2"])
	}
}

func TestHandleDeleteContextValuesByRequestIDWithKeys(t *testing.T) {
	rec := recorder.New()
	s := New(rec, "9000")
	r := s.router()

	// Seed via PUT /context-values/req-1 (flat object, not nested)
	payload := `{"key1": "val1", "key2": "val2", "key3": "val3"}`
	req := httptest.NewRequest(http.MethodPut, "/context-values/req-1", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Delete specific keys
	payload = `{"keys": ["key1", "key3"]}`
	req = httptest.NewRequest(http.MethodDelete, "/context-values/req-1", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	// Verify only key2 remains
	req = httptest.NewRequest(http.MethodGet, "/context-values/req-1", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	resp = w.Result()
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}
	if _, ok := result["key1"]; ok {
		t.Error("Expected key1 to be deleted")
	}
	if result["key2"] != "val2" {
		t.Errorf("Expected key2=val2, got %v", result["key2"])
	}
	if _, ok := result["key3"]; ok {
		t.Error("Expected key3 to be deleted")
	}
}

func TestContextValuesSharedStore(t *testing.T) {
	// Verify that WithContextValues shares the same store instance
	rec := recorder.New()
	store := mm.NewStore()
	store.Replace("shared-req", map[string]any{"shared_key": "shared_val"})

	s := New(rec, "9000", WithContextValues(store))
	r := s.router()

	// GET should see the pre-seeded data
	req := httptest.NewRequest(http.MethodGet, "/context-values/shared-req", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}
	if result["shared_key"] != "shared_val" {
		t.Errorf("Expected shared_key=shared_val, got %v", result["shared_key"])
	}
}

// --- Service docs discovery tests ---

func TestHandleDocsNoServiceInfo(t *testing.T) {
	rec := recorder.New()
	s := New(rec, "9000")
	r := s.router()

	req := httptest.NewRequest(http.MethodGet, "/docs", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}
	items, ok := result["items"].([]any)
	if !ok {
		t.Fatalf("Expected 'items' array, got %T", result["items"])
	}
	if len(items) != 0 {
		t.Errorf("Expected empty items, got %d", len(items))
	}
}

func TestHandleDocsWithServiceInfo(t *testing.T) {
	rec := recorder.New()
	s := New(rec, "9000", WithServiceInfo(func() map[string]grpc.ServiceInfo {
		return map[string]grpc.ServiceInfo{
			"test.EchoService": {
				Methods: []grpc.MethodInfo{
					{Name: "/test.EchoService/Echo"},
					{Name: "/test.EchoService/Stream"},
				},
			},
			"test.ComplexService": {
				Methods: []grpc.MethodInfo{
					{Name: "/test.ComplexService/Create"},
				},
			},
		}
	}))
	r := s.router()

	req := httptest.NewRequest(http.MethodGet, "/docs", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}
	items, ok := result["items"].([]any)
	if !ok {
		t.Fatalf("Expected 'items' array, got %T", result["items"])
	}
	if len(items) != 2 {
		t.Fatalf("Expected 2 services, got %d", len(items))
	}
}

func TestHandleDocsServiceFound(t *testing.T) {
	rec := recorder.New()
	s := New(rec, "9000", WithServiceInfo(func() map[string]grpc.ServiceInfo {
		return map[string]grpc.ServiceInfo{
			"test.EchoService": {
				Methods: []grpc.MethodInfo{
					{Name: "/test.EchoService/Echo"},
					{Name: "/test.EchoService/Stream"},
				},
			},
		}
	}))
	r := s.router()

	req := httptest.NewRequest(http.MethodGet, "/docs/test.EchoService", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}
	if result["name"] != "test.EchoService" {
		t.Errorf("Expected name 'test.EchoService', got %v", result["name"])
	}
	methods, ok := result["methods"].([]any)
	if !ok {
		t.Fatalf("Expected 'methods' array, got %T", result["methods"])
	}
	if len(methods) != 2 {
		t.Errorf("Expected 2 methods, got %d", len(methods))
	}
}

func TestHandleDocsServiceNotFound(t *testing.T) {
	rec := recorder.New()
	s := New(rec, "9000", WithServiceInfo(func() map[string]grpc.ServiceInfo {
		return map[string]grpc.ServiceInfo{
			"test.EchoService": {Methods: []grpc.MethodInfo{{Name: "/test.EchoService/Echo"}}},
		}
	}))
	r := s.router()

	req := httptest.NewRequest(http.MethodGet, "/docs/nonexistent.Service", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("Expected status 404, got %d", resp.StatusCode)
	}
}

func TestHandleDocsServiceNoServiceInfo(t *testing.T) {
	rec := recorder.New()
	s := New(rec, "9000")
	r := s.router()

	req := httptest.NewRequest(http.MethodGet, "/docs/test.EchoService", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("Expected status 404, got %d", resp.StatusCode)
	}
}
