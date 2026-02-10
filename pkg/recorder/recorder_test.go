package recorder

import (
	"encoding/json"
	"sync"
	"testing"
	"time"
)

func TestNew(t *testing.T) {
	r := New()
	if r == nil {
		t.Fatal("New() returned nil")
	}
	if len(r.GetRecords()) != 0 {
		t.Error("New recorder should have no records")
	}
}

func TestRecord(t *testing.T) {
	r := New()

	record := CallRecord{
		RequestID:  "test-id-1",
		Method:     "/TestService/TestMethod",
		Timestamp:  time.Now(),
		Request:    map[string]string{"message": "hello"},
		Response:   map[string]string{"message": "world"},
		DurationMs: 100,
	}

	r.Record(record)

	records := r.GetRecords()
	if len(records) != 1 {
		t.Fatalf("Expected 1 record, got %d", len(records))
	}

	if records[0].RequestID != "test-id-1" {
		t.Errorf("Expected request_id 'test-id-1', got '%s'", records[0].RequestID)
	}
	if records[0].Method != "/TestService/TestMethod" {
		t.Errorf("Expected method '/TestService/TestMethod', got '%s'", records[0].Method)
	}
}

func TestRecordMultiple(t *testing.T) {
	r := New()

	for i := 0; i < 5; i++ {
		r.Record(CallRecord{
			RequestID: "test-id",
			Method:    "/TestService/TestMethod",
			Timestamp: time.Now(),
		})
	}

	records := r.GetRecords()
	if len(records) != 5 {
		t.Fatalf("Expected 5 records, got %d", len(records))
	}
}

func TestClear(t *testing.T) {
	r := New()

	r.Record(CallRecord{
		RequestID: "test-id",
		Method:    "/TestService/TestMethod",
		Timestamp: time.Now(),
	})

	if len(r.GetRecords()) != 1 {
		t.Fatal("Expected 1 record before clear")
	}

	r.Clear()

	if len(r.GetRecords()) != 0 {
		t.Error("Expected 0 records after clear")
	}
}

func TestToJSON(t *testing.T) {
	r := New()

	timestamp := time.Date(2026, 2, 10, 12, 0, 0, 0, time.UTC)
	r.Record(CallRecord{
		RequestID:  "test-id",
		Method:     "/TestService/TestMethod",
		Timestamp:  timestamp,
		Request:    map[string]string{"message": "hello"},
		Response:   map[string]string{"message": "world"},
		DurationMs: 50,
	})

	data, err := r.ToJSON()
	if err != nil {
		t.Fatalf("ToJSON() error: %v", err)
	}

	var records []CallRecord
	if err := json.Unmarshal(data, &records); err != nil {
		t.Fatalf("Failed to unmarshal JSON: %v", err)
	}

	if len(records) != 1 {
		t.Fatalf("Expected 1 record, got %d", len(records))
	}

	if records[0].RequestID != "test-id" {
		t.Errorf("Expected request_id 'test-id', got '%s'", records[0].RequestID)
	}
}

func TestRecordWithError(t *testing.T) {
	r := New()

	r.Record(CallRecord{
		RequestID:  "test-id",
		Method:     "/TestService/TestMethod",
		Timestamp:  time.Now(),
		Request:    map[string]string{"message": "hello"},
		Error:      "something went wrong",
		DurationMs: 10,
	})

	records := r.GetRecords()
	if len(records) != 1 {
		t.Fatalf("Expected 1 record, got %d", len(records))
	}

	if records[0].Error != "something went wrong" {
		t.Errorf("Expected error 'something went wrong', got '%s'", records[0].Error)
	}
}

func TestRecordWithPanic(t *testing.T) {
	r := New()

	r.Record(CallRecord{
		RequestID:  "test-id",
		Method:     "/TestService/TestMethod",
		Timestamp:  time.Now(),
		Request:    map[string]string{"message": "hello"},
		Panic:      "runtime error: index out of range",
		DurationMs: 5,
	})

	records := r.GetRecords()
	if len(records) != 1 {
		t.Fatalf("Expected 1 record, got %d", len(records))
	}

	if records[0].Panic != "runtime error: index out of range" {
		t.Errorf("Expected panic 'runtime error: index out of range', got '%s'", records[0].Panic)
	}
}

func TestConcurrentAccess(t *testing.T) {
	r := New()
	var wg sync.WaitGroup

	// Concurrent writes
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			r.Record(CallRecord{
				RequestID: "test-id",
				Method:    "/TestService/TestMethod",
				Timestamp: time.Now(),
			})
		}(i)
	}

	// Concurrent reads
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = r.GetRecords()
		}()
	}

	// Concurrent clears
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.Clear()
		}()
	}

	wg.Wait()

	// Should not panic and should complete without data races
}

func TestGetRecordsReturnsCopy(t *testing.T) {
	r := New()

	r.Record(CallRecord{
		RequestID: "test-id-1",
		Method:    "/TestService/TestMethod",
		Timestamp: time.Now(),
	})

	records1 := r.GetRecords()
	records1[0].RequestID = "modified"

	records2 := r.GetRecords()
	if records2[0].RequestID != "test-id-1" {
		t.Error("GetRecords should return a copy, not the original slice")
	}
}
