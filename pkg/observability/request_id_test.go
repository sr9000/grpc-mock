package observability

import "testing"

func TestResolveRequestIDFromHeaders(t *testing.T) {
	headers := []string{"x-request-id", "x-correlation-id"}

	tests := []struct {
		name      string
		getHeader func(string) string
		want      string
	}{
		{
			name: "first header present",
			getHeader: func(key string) string {
				if key == "x-request-id" {
					return "abc-123"
				}
				return ""
			},
			want: "abc-123",
		},
		{
			name: "second header present",
			getHeader: func(key string) string {
				if key == "x-correlation-id" {
					return "corr-456"
				}
				return ""
			},
			want: "corr-456",
		},
		{
			name: "no headers present - generates",
			getHeader: func(key string) string {
				return ""
			},
		},
		{
			name: "whitespace-only header - generates",
			getHeader: func(key string) string {
				if key == "x-request-id" {
					return "  "
				}
				return ""
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveRequestID(tt.getHeader, headers)
			if tt.want != "" && got != tt.want {
				t.Errorf("ResolveRequestID() = %q, want %q", got, tt.want)
			}
			if tt.want == "" && got == "" {
				t.Error("ResolveRequestID() should generate an ID when no header matches")
			}
		})
	}
}

func TestGenerateRequestIDUniqueness(t *testing.T) {
	ids := make(map[string]bool)
	for i := 0; i < 100; i++ {
		id := GenerateRequestID()
		if ids[id] {
			t.Fatalf("duplicate request ID generated: %s", id)
		}
		ids[id] = true
	}
}

func TestNormalizeHeaderList(t *testing.T) {
	defaults := []string{"x-request-id", "x-correlation-id"}

	tests := []struct {
		name    string
		csv     string
		wantLen int
		want0   string
	}{
		{"empty uses defaults", "", 2, "x-request-id"},
		{"whitespace uses defaults", "  ", 2, "x-request-id"},
		{"single header", "x-custom-id", 1, "x-custom-id"},
		{"multiple headers", "x-req-id, x-corr-id", 2, "x-req-id"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NormalizeHeaderList(tt.csv, defaults)
			if len(result) != tt.wantLen {
				t.Errorf("got %d headers, want %d", len(result), tt.wantLen)
			}
			if result[0] != tt.want0 {
				t.Errorf("got first header %q, want %q", result[0], tt.want0)
			}
		})
	}
}

func TestTraceIDFromTraceparent(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"valid", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01", "4bf92f3577b34da6a3ce929d0e0e4736"},
		{"invalid parts count", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7", ""},
		{"invalid trace-id length", "00-4bf92f-00f067aa0ba902b7-01", ""},
		{"empty", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TraceIDFromTraceparent(tt.input)
			if got != tt.want {
				t.Errorf("TraceIDFromTraceparent(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
