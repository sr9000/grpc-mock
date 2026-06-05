package metrics

import (
	"fmt"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestErrorKindFromErr(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantKind string
	}{
		{
			name:     "nil error returns OK",
			err:      nil,
			wantKind: codes.OK.String(),
		},
		{
			name:     "gRPC status error returns code string",
			err:      status.Error(codes.NotFound, "item not found"),
			wantKind: codes.NotFound.String(),
		},
		{
			name:     "gRPC Internal error",
			err:      status.Error(codes.Internal, "something broke"),
			wantKind: codes.Internal.String(),
		},
		{
			name:     "gRPC PermissionDenied error",
			err:      status.Error(codes.PermissionDenied, "forbidden"),
			wantKind: codes.PermissionDenied.String(),
		},
		{
			name:     "non-gRPC error returns Unknown",
			err:      fmt.Errorf("plain error"),
			wantKind: codes.Unknown.String(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := errorKindFromErr(tt.err)
			if got != tt.wantKind {
				t.Errorf("errorKindFromErr() = %q, want %q", got, tt.wantKind)
			}
		})
	}
}

func TestPanicKind(t *testing.T) {
	tests := []struct {
		name     string
		msg      string
		wantKind string
	}{
		{
			name:     "nil pointer dereference",
			msg:      "runtime error: invalid memory address or nil pointer dereference",
			wantKind: "nil_pointer",
		},
		{
			name:     "index out of range",
			msg:      "runtime error: index out of range [5] with length 3",
			wantKind: "index_out_of_range",
		},
		{
			name:     "slice bounds out of range",
			msg:      "runtime error: slice bounds out of range [10:]",
			wantKind: "slice_bounds",
		},
		{
			name:     "interface conversion error",
			msg:      "interface conversion: interface {} is string, not int",
			wantKind: "type_assertion",
		},
		{
			name:     "concurrent map write",
			msg:      "fatal error: concurrent map writes",
			wantKind: "concurrent_map",
		},
		{
			name:     "unknown panic falls back to runtime_error",
			msg:      "something completely unexpected",
			wantKind: "runtime_error",
		},
		{
			name:     "empty message falls back to runtime_error",
			msg:      "",
			wantKind: "runtime_error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := panicKind(tt.msg)
			if got != tt.wantKind {
				t.Errorf("panicKind() = %q, want %q", got, tt.wantKind)
			}
		})
	}
}

func TestErrorKindBounded(t *testing.T) {
	// Verify that all gRPC status codes produce bounded label values.
	for _, code := range []codes.Code{
		codes.OK, codes.Canceled, codes.Unknown, codes.InvalidArgument,
		codes.DeadlineExceeded, codes.NotFound, codes.AlreadyExists,
		codes.PermissionDenied, codes.ResourceExhausted, codes.FailedPrecondition,
		codes.Aborted, codes.OutOfRange, codes.Unimplemented, codes.Internal,
		codes.Unavailable, codes.DataLoss, codes.Unauthenticated,
	} {
		err := status.Error(code, "test")
		kind := errorKindFromErr(err)
		if kind != code.String() {
			t.Errorf("errorKindFromErr(status.Error(%v)) = %q, want %q", code, kind, code.String())
		}
	}
}

func TestPanicKindBounded(t *testing.T) {
	// Verify that panicKind always returns one of the fixed set of values.
	allowedKinds := map[string]bool{
		"nil_pointer":        true,
		"index_out_of_range": true,
		"slice_bounds":       true,
		"type_assertion":     true,
		"concurrent_map":     true,
		"runtime_error":      true,
	}

	// Test with various arbitrary panic messages
	messages := []string{
		"some random panic message",
		"another kind of error",
		"stack overflow",
		"out of memory",
		"deadlock",
	}
	for _, msg := range messages {
		kind := panicKind(msg)
		if !allowedKinds[kind] {
			t.Errorf("panicKind(%q) = %q, not in allowed set", msg, kind)
		}
	}
}
