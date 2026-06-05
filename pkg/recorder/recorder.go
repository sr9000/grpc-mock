package recorder

import (
	"encoding/json"
	"sync"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// CallRecord represents a single gRPC call record
type CallRecord struct {
	RequestID  string          `json:"request_id"`
	Method     string          `json:"method"`
	Timestamp  time.Time       `json:"timestamp"`
	Request    json.RawMessage `json:"request,omitempty"`
	Response   json.RawMessage `json:"response,omitempty"`
	Error      string          `json:"error,omitempty"`
	Panic      string          `json:"panic,omitempty"`
	DurationMs int64           `json:"duration_ms"`
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

// GetRecordsByRequestID returns recorded calls that match requestID.
func (r *Recorder) GetRecordsByRequestID(requestID string) []CallRecord {
	all := r.GetRecords()
	filtered := make([]CallRecord, 0)
	for _, record := range all {
		if record.RequestID == requestID {
			filtered = append(filtered, record)
		}
	}
	return filtered
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

// MarshalProto marshals a proto message to json.RawMessage using protojson,
// falling back to encoding/json for non-proto values.
func MarshalProto(v any) json.RawMessage {
	if v == nil {
		return nil
	}
	// Try protojson.Marshal first (handles proto messages correctly).
	if msg, ok := v.(proto.Message); ok {
		b, err := protojson.Marshal(msg)
		if err == nil {
			return b
		}
	}
	// Fallback to standard JSON encoding.
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return b
}
