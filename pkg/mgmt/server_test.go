package mgmt

import (
	"encoding/json"
	"grpc-mock/pkg/recorder"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
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

	methods := []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch}

	for _, method := range methods {
		req := httptest.NewRequest(method, "/logs", nil)
		w := httptest.NewRecorder()

		s.handleLogs(w, req)

		resp := w.Result()
		resp.Body.Close()

		if resp.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("Expected status 405 for %s, got %d", method, resp.StatusCode)
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

	methods := []string{http.MethodGet, http.MethodPut, http.MethodPatch}

	for _, method := range methods {
		req := httptest.NewRequest(method, "/clear", nil)
		w := httptest.NewRecorder()

		s.handleClear(w, req)

		resp := w.Result()
		resp.Body.Close()

		if resp.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("Expected status 405 for %s, got %d", method, resp.StatusCode)
		}
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

	req := httptest.NewRequest(http.MethodPost, "/doc", nil)
	w := httptest.NewRecorder()

	s.handleDoc(w, req)

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
