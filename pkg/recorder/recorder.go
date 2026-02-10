package recorder

import (
	"encoding/json"
	"sync"
	"time"
)

// CallRecord represents a single gRPC call record
type CallRecord struct {
	RequestID  string    `json:"request_id"`
	Method     string    `json:"method"`
	Timestamp  time.Time `json:"timestamp"`
	Request    any       `json:"request"`
	Response   any       `json:"response,omitempty"`
	Error      string    `json:"error,omitempty"`
	Panic      string    `json:"panic,omitempty"`
	DurationMs int64     `json:"duration_ms"`
}

// Recorder stores gRPC call records in memory
type Recorder struct {
	mu      sync.RWMutex
	records []CallRecord
}

// New creates a new Recorder instance
func New() *Recorder {
	return &Recorder{
		records: make([]CallRecord, 0),
	}
}

// Record adds a new call record
func (r *Recorder) Record(record CallRecord) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.records = append(r.records, record)
}

// GetRecords returns all recorded calls
func (r *Recorder) GetRecords() []CallRecord {
	r.mu.RLock()
	defer r.mu.RUnlock()
	// Return a copy to avoid race conditions
	result := make([]CallRecord, len(r.records))
	copy(result, r.records)
	return result
}

// Clear removes all recorded calls
func (r *Recorder) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.records = make([]CallRecord, 0)
}

// ToJSON returns the records as JSON bytes
func (r *Recorder) ToJSON() ([]byte, error) {
	records := r.GetRecords()
	return json.Marshal(records)
}
