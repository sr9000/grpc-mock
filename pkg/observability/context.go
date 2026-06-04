package observability

import (
	"context"

	"github.com/rs/zerolog"
)

type requestIDKey struct{}
type traceIDKey struct{}
type methodKey struct{}
type loggerKey struct{}
type requestMetadataKey struct{}

// RequestMetadata groups per-request correlation fields.
type RequestMetadata struct {
	RequestID string
	TraceID   string
	Method    string
}

// EnsureRequestMetadata returns the metadata from ctx or a zero-value struct.
func EnsureRequestMetadata(ctx context.Context) *RequestMetadata {
	if md, ok := ctx.Value(requestMetadataKey{}).(*RequestMetadata); ok && md != nil {
		return md
	}
	return &RequestMetadata{}
}

// WithRequestMetadata stores metadata in ctx.
func WithRequestMetadata(ctx context.Context, metadata *RequestMetadata) context.Context {
	if metadata == nil {
		metadata = &RequestMetadata{}
	}
	return context.WithValue(ctx, requestMetadataKey{}, metadata)
}

// RequestMetadataFromContext returns the stored metadata (may be nil).
func RequestMetadataFromContext(ctx context.Context) *RequestMetadata {
	md, _ := ctx.Value(requestMetadataKey{}).(*RequestMetadata)
	return md
}

// WithRequestID stores requestID in both the dedicated key and RequestMetadata.
func WithRequestID(ctx context.Context, requestID string) context.Context {
	if md := RequestMetadataFromContext(ctx); md != nil {
		md.RequestID = requestID
	}
	return context.WithValue(ctx, requestIDKey{}, requestID)
}

// RequestID returns the request ID from context.
func RequestID(ctx context.Context) string {
	if md := RequestMetadataFromContext(ctx); md != nil && md.RequestID != "" {
		return md.RequestID
	}
	v, _ := ctx.Value(requestIDKey{}).(string)
	return v
}

// WithTraceID stores traceID in both the dedicated key and RequestMetadata.
func WithTraceID(ctx context.Context, traceID string) context.Context {
	if md := RequestMetadataFromContext(ctx); md != nil {
		md.TraceID = traceID
	}
	return context.WithValue(ctx, traceIDKey{}, traceID)
}

// TraceID returns the trace ID from context.
func TraceID(ctx context.Context) string {
	if md := RequestMetadataFromContext(ctx); md != nil && md.TraceID != "" {
		return md.TraceID
	}
	v, _ := ctx.Value(traceIDKey{}).(string)
	return v
}

// WithMethod stores the gRPC method in both the dedicated key and RequestMetadata.
func WithMethod(ctx context.Context, method string) context.Context {
	if md := RequestMetadataFromContext(ctx); md != nil {
		md.Method = method
	}
	return context.WithValue(ctx, methodKey{}, method)
}

// Method returns the gRPC method from context.
func Method(ctx context.Context) string {
	if md := RequestMetadataFromContext(ctx); md != nil && md.Method != "" {
		return md.Method
	}
	v, _ := ctx.Value(methodKey{}).(string)
	return v
}

// WithLogger stores a zerolog.Logger in context.
func WithLogger(ctx context.Context, logger zerolog.Logger) context.Context {
	return context.WithValue(ctx, loggerKey{}, logger)
}

// Logger returns the logger from context or the fallback.
func Logger(ctx context.Context, fallback zerolog.Logger) zerolog.Logger {
	v := ctx.Value(loggerKey{})
	if l, ok := v.(zerolog.Logger); ok {
		return l
	}
	return fallback
}
