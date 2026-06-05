package observability

import (
	"context"
	"testing"

	"github.com/rs/zerolog"
)

func TestRequestMetadataRoundTrip(t *testing.T) {
	ctx := context.Background()
	md := &RequestMetadata{RequestID: "r1", TraceID: "t1", Method: "m1"}
	ctx = WithRequestMetadata(ctx, md)

	got := RequestMetadataFromContext(ctx)
	if got == nil {
		t.Fatal("expected metadata, got nil")
	}
	if got.RequestID != "r1" || got.TraceID != "t1" || got.Method != "m1" {
		t.Errorf("metadata mismatch: %+v", got)
	}
}

func TestEnsureRequestMetadataMissing(t *testing.T) {
	ctx := context.Background()
	md := EnsureRequestMetadata(ctx)
	if md == nil {
		t.Fatal("expected non-nil metadata")
	}
	if md.RequestID != "" || md.TraceID != "" || md.Method != "" {
		t.Errorf("expected zero metadata, got %+v", md)
	}
}

func TestRequestIDRoundTrip(t *testing.T) {
	ctx := context.Background()
	ctx = WithRequestMetadata(ctx, &RequestMetadata{})
	ctx = WithRequestID(ctx, "abc-123")

	if got := RequestID(ctx); got != "abc-123" {
		t.Errorf("expected abc-123, got %q", got)
	}

	// Also stored in metadata
	md := RequestMetadataFromContext(ctx)
	if md.RequestID != "abc-123" {
		t.Errorf("metadata request_id = %q, want abc-123", md.RequestID)
	}
}

func TestTraceIDRoundTrip(t *testing.T) {
	ctx := context.Background()
	ctx = WithRequestMetadata(ctx, &RequestMetadata{})
	ctx = WithTraceID(ctx, "trace-456")

	if got := TraceID(ctx); got != "trace-456" {
		t.Errorf("expected trace-456, got %q", got)
	}
}

func TestMethodRoundTrip(t *testing.T) {
	ctx := context.Background()
	ctx = WithRequestMetadata(ctx, &RequestMetadata{})
	ctx = WithMethod(ctx, "/pkg.Service/Method")

	if got := Method(ctx); got != "/pkg.Service/Method" {
		t.Errorf("expected /pkg.Service/Method, got %q", got)
	}
}

func TestRequestIDWithoutMetadata(t *testing.T) {
	ctx := context.Background()
	ctx = WithRequestID(ctx, "standalone")

	if got := RequestID(ctx); got != "standalone" {
		t.Errorf("expected standalone, got %q", got)
	}
}

func TestTraceIDWithoutMetadata(t *testing.T) {
	ctx := context.Background()
	ctx = WithTraceID(ctx, "standalone-trace")

	if got := TraceID(ctx); got != "standalone-trace" {
		t.Errorf("expected standalone-trace, got %q", got)
	}
}

func TestWithLoggerRoundTrip(t *testing.T) {
	base := zerolog.Nop()
	ctx := WithLogger(context.Background(), base)

	got := Logger(ctx, zerolog.Nop())
	// zerolog.Logger contains []byte so we can't use ==.
	// Verify the logger is usable and not a zero-value.
	if got.GetLevel() != base.GetLevel() {
		t.Errorf("expected level %v, got %v", base.GetLevel(), got.GetLevel())
	}
}

func TestLoggerFallback(t *testing.T) {
	fallback := zerolog.Nop()
	got := Logger(context.Background(), fallback)
	// Can't compare zerolog.Logger structs directly; verify level matches.
	if got.GetLevel() != fallback.GetLevel() {
		t.Errorf("expected level %v, got %v", fallback.GetLevel(), got.GetLevel())
	}
}

func TestWithRequestMetadataNil(t *testing.T) {
	ctx := WithRequestMetadata(context.Background(), nil)
	md := RequestMetadataFromContext(ctx)
	if md == nil {
		t.Fatal("expected non-nil metadata after WithRequestMetadata(nil)")
	}
}
