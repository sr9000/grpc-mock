package observability

import (
	"crypto/rand"
	"fmt"
	"strings"
	"time"
)

// DefaultRequestIDHeaders lists the metadata keys checked for an inbound request ID.
var DefaultRequestIDHeaders = []string{"x-request-id", "x-correlation-id"}

// DefaultRequestIDResponseHeader is the header used to echo the request ID back.
const DefaultRequestIDResponseHeader = "x-request-id"

// NormalizeHeaderList splits a CSV of header names, falling back to defaults when empty.
func NormalizeHeaderList(headersCSV string, defaults []string) []string {
	if strings.TrimSpace(headersCSV) == "" {
		return defaults
	}
	parts := strings.Split(headersCSV, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		h := strings.TrimSpace(p)
		if h != "" {
			result = append(result, h)
		}
	}
	if len(result) == 0 {
		return defaults
	}
	return result
}

// ResolveRequestID walks allowedHeaders using getHeader, returning the first
// non-empty value. Falls back to GenerateRequestID when none match.
func ResolveRequestID(getHeader func(string) string, allowedHeaders []string) string {
	for _, h := range allowedHeaders {
		v := strings.TrimSpace(getHeader(h))
		if v != "" {
			return v
		}
	}
	return GenerateRequestID()
}

// GenerateRequestID creates a random 16-char hex string.
func GenerateRequestID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		now := time.Now().UnixNano()
		for i := 0; i < len(b); i++ {
			b[i] = byte(now >> (8 * i))
		}
	}
	return fmt.Sprintf("%x", b)
}

// TraceIDFromTraceparent extracts the trace-id portion from a W3C traceparent header.
func TraceIDFromTraceparent(v string) string {
	parts := strings.Split(strings.TrimSpace(v), "-")
	if len(parts) != 4 {
		return ""
	}
	if len(parts[1]) != 32 {
		return ""
	}
	return strings.ToLower(parts[1])
}
