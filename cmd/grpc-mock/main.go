package main

import (
	"context"
	"fmt"
	"grpc-mock/internal/app"
	"grpc-mock/pkg/metrics"
	"grpc-mock/pkg/mgmt"
	"grpc-mock/pkg/mm"
	"grpc-mock/pkg/observability"
	"grpc-mock/pkg/recorder"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/caarlos0/env/v11"
	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/reflection"
)

type Config struct {
	Host                    string  `env:"HOST" envDefault:"0.0.0.0"`
	Port                    string  `env:"PORT" envDefault:"50051"`
	MgmtPort                string  `env:"MGMT_PORT" envDefault:"9000"`
	MetricsPort             string  `env:"METRICS_PORT" envDefault:"9100"`
	EnableMgmt              bool    `env:"MGMT_ENABLED" envDefault:"true"`
	EnableMetrics           bool    `env:"METRICS_ENABLED" envDefault:"true"`
	EnableReflection        bool    `env:"GRPC_REFLECTION" envDefault:"false"`
	EnableLogging           bool    `env:"GRPC_LOGGING" envDefault:"true"`
	LogFormat               string  `env:"LOG_FORMAT" envDefault:"json"`
	LogOutput               string  `env:"LOG_OUTPUT" envDefault:"stdout"`
	LogFile                 string  `env:"LOG_FILE"`
	LogLevel                string  `env:"LOG_LEVEL" envDefault:"info"`
	RequestIDHeaders        string  `env:"REQUEST_ID_HEADERS"`
	RequestIDResponseHeader string  `env:"REQUEST_ID_RESPONSE_HEADER" envDefault:"x-request-id"`
	TraceEnabled            bool    `env:"TRACE_ENABLED" envDefault:"false"`
	TraceExporter           string  `env:"TRACE_EXPORTER" envDefault:"none"`
	TraceEndpoint           string  `env:"TRACE_ENDPOINT"`
	TraceFile               string  `env:"TRACE_FILE" envDefault:"./traces.json"`
	TraceSamplingRatio      float64 `env:"TRACE_SAMPLING_RATIO" envDefault:"1.0"`
}

func loadConfig() (Config, error) {
	var cfg Config
	err := env.Parse(&cfg)
	return cfg, err
}

// version is printed by the `version` command; set at build time if desired.
var version = "dev"

func main() {
	rootCmd := &cobra.Command{
		Use:   "grpc-stub",
		Short: "gRPC stub server",
		Long:  "gRPC stub server with configurable host, port, and reflection.",
		// no RunE here: default is to show help when called without subcommand
	}

	runCmd := &cobra.Command{
		Use:   "run [host] [port]",
		Short: "Run the gRPC stub server",
		Args:  cobra.MaximumNArgs(2),
		RunE:  runServer,
	}

	// Flags prefer command args; env vars are only fallbacks inside runServer.
	runCmd.Flags().StringP("host", "", "", "Host interface to bind (overrides HOST env var)")
	runCmd.Flags().StringP("port", "p", "", "Port to listen on (overrides PORT env var)")
	runCmd.Flags().StringP("mgmt-port", "m", "", "Management server port (overrides MGMT_PORT env var)")
	runCmd.Flags().StringP("metrics-port", "", "", "Metrics server port (overrides METRICS_PORT env var)")
	runCmd.Flags().Bool("mgmt-enabled", true, "Enable management server (overrides MGMT_ENABLED env var)")
	runCmd.Flags().Bool("metrics-enabled", true, "Enable metrics server (overrides METRICS_ENABLED env var)")
	runCmd.Flags().Bool("logging", true, "Enable gRPC request logging (overrides GRPC_LOGGING env var)")
	runCmd.Flags().BoolP("reflection", "r", false, "Enable gRPC server reflection (overrides GRPC_REFLECTION env var)")

	// Logging flags
	runCmd.Flags().String("log-format", "", "Log format: json or console (overrides LOG_FORMAT env var)")
	runCmd.Flags().String("log-output", "", "Log output: stdout or file (overrides LOG_OUTPUT env var)")
	runCmd.Flags().String("log-file", "", "Log file path when output=file (overrides LOG_FILE env var)")
	runCmd.Flags().String("log-level", "", "Log level: debug, info, warn, error (overrides LOG_LEVEL env var)")

	// Tracing flags
	runCmd.Flags().Bool("trace-enabled", false, "Enable OpenTelemetry tracing (overrides TRACE_ENABLED env var)")
	runCmd.Flags().String("trace-exporter", "", "Trace exporter: none, file, otlp-http (overrides TRACE_EXPORTER env var)")
	runCmd.Flags().String("trace-endpoint", "", "OTLP HTTP endpoint, e.g. otel-collector:4318 (overrides TRACE_ENDPOINT env var)")
	runCmd.Flags().String("trace-file", "", "Trace file path when exporter=file (overrides TRACE_FILE env var)")
	runCmd.Flags().Float64("trace-sampling-ratio", 0, "Trace sampling ratio 0.0–1.0 (overrides TRACE_SAMPLING_RATIO env var)")

	// Request-ID flags
	runCmd.Flags().String("request-id-headers", "", "Comma-separated list of request-id header names (overrides REQUEST_ID_HEADERS env var)")
	runCmd.Flags().String("request-id-response-header", "", "Response header name for request-id echo (overrides REQUEST_ID_RESPONSE_HEADER env var)")

	// Deprecated flags (kept for backward compatibility)
	runCmd.Flags().Bool("no-mgmt", false, "[deprecated] Use --mgmt-enabled=false instead")
	runCmd.Flags().Bool("no-metrics", false, "[deprecated] Use --metrics-enabled=false instead")
	runCmd.Flags().Bool("no-logs", false, "[deprecated] Use --logging=false instead")

	versionCmd := &cobra.Command{
		Use:   "version",
		Short: "Print the version information",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println(version)
		},
	}

	rootCmd.AddCommand(runCmd)
	rootCmd.AddCommand(versionCmd)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func runServer(cmd *cobra.Command, args []string) error {
	// Load env-based defaults.
	cfg, err := loadConfig()
	if err != nil {
		return fmt.Errorf("failed to parse config from env: %w", err)
	}

	// Override with flags when provided.
	if v, _ := cmd.Flags().GetString("host"); v != "" {
		cfg.Host = v
	}
	if v, _ := cmd.Flags().GetString("port"); v != "" {
		cfg.Port = v
	}
	if v, _ := cmd.Flags().GetString("mgmt-port"); v != "" {
		cfg.MgmtPort = v
	}
	if v, _ := cmd.Flags().GetString("metrics-port"); v != "" {
		cfg.MetricsPort = v
	}
	if cmd.Flags().Changed("mgmt-enabled") {
		v, _ := cmd.Flags().GetBool("mgmt-enabled")
		cfg.EnableMgmt = v
	}
	if cmd.Flags().Changed("metrics-enabled") {
		v, _ := cmd.Flags().GetBool("metrics-enabled")
		cfg.EnableMetrics = v
	}
	if cmd.Flags().Changed("logging") {
		v, _ := cmd.Flags().GetBool("logging")
		cfg.EnableLogging = v
	}
	if cmd.Flags().Changed("reflection") {
		v, _ := cmd.Flags().GetBool("reflection")
		cfg.EnableReflection = v
	}

	// Logging flag overrides
	if v, _ := cmd.Flags().GetString("log-format"); v != "" {
		cfg.LogFormat = v
	}
	if v, _ := cmd.Flags().GetString("log-output"); v != "" {
		cfg.LogOutput = v
	}
	if v, _ := cmd.Flags().GetString("log-file"); v != "" {
		cfg.LogFile = v
	}
	if v, _ := cmd.Flags().GetString("log-level"); v != "" {
		cfg.LogLevel = v
	}

	// Tracing flag overrides
	if cmd.Flags().Changed("trace-enabled") {
		v, _ := cmd.Flags().GetBool("trace-enabled")
		cfg.TraceEnabled = v
	}
	if v, _ := cmd.Flags().GetString("trace-exporter"); v != "" {
		cfg.TraceExporter = v
	}
	if v, _ := cmd.Flags().GetString("trace-endpoint"); v != "" {
		cfg.TraceEndpoint = v
	}
	if v, _ := cmd.Flags().GetString("trace-file"); v != "" {
		cfg.TraceFile = v
	}
	if cmd.Flags().Changed("trace-sampling-ratio") {
		v, _ := cmd.Flags().GetFloat64("trace-sampling-ratio")
		cfg.TraceSamplingRatio = v
	}

	// Request-ID flag overrides
	if v, _ := cmd.Flags().GetString("request-id-headers"); v != "" {
		cfg.RequestIDHeaders = v
	}
	if v, _ := cmd.Flags().GetString("request-id-response-header"); v != "" {
		cfg.RequestIDResponseHeader = v
	}

	// Deprecated flag handling (lower precedence than explicit boolean flags)
	if cmd.Flags().Changed("no-mgmt") && !cmd.Flags().Changed("mgmt-enabled") {
		v, _ := cmd.Flags().GetBool("no-mgmt")
		cfg.EnableMgmt = !v
	}
	if cmd.Flags().Changed("no-metrics") && !cmd.Flags().Changed("metrics-enabled") {
		v, _ := cmd.Flags().GetBool("no-metrics")
		cfg.EnableMetrics = !v
	}
	if cmd.Flags().Changed("no-logs") && !cmd.Flags().Changed("logging") {
		v, _ := cmd.Flags().GetBool("no-logs")
		cfg.EnableLogging = !v
	}

	// Positional args have highest precedence: run [host] [port]
	if len(args) >= 1 && args[0] != "" {
		cfg.Host = args[0]
	}
	if len(args) >= 2 && args[1] != "" {
		cfg.Port = args[1]
	}

	// Build the base structured logger from config.
	baseLogger, logCloser, err := observability.NewLogger(observability.LogConfig{
		Format: cfg.LogFormat,
		Output: cfg.LogOutput,
		File:   cfg.LogFile,
		Level:  cfg.LogLevel,
	})
	if err != nil {
		return fmt.Errorf("failed to create logger: %w", err)
	}
	if logCloser != nil {
		defer logCloser.Close()
	}

	// Initialise OpenTelemetry tracing.
	traceShutdown, err := observability.SetupTracing(context.Background(), observability.TraceConfig{
		Enabled:       cfg.TraceEnabled,
		Exporter:      cfg.TraceExporter,
		Endpoint:      cfg.TraceEndpoint,
		File:          cfg.TraceFile,
		SamplingRatio: cfg.TraceSamplingRatio,
		ServiceName:   "grpc-mock",
	})
	if err != nil {
		return fmt.Errorf("failed to initialize tracing: %w", err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = traceShutdown(shutdownCtx)
	}()

	addr := net.JoinHostPort(cfg.Host, cfg.Port)

	// Log effective configuration before starting.
	baseLogger.Info().
		Str("host", cfg.Host).
		Str("port", cfg.Port).
		Str("mgmt_port", cfg.MgmtPort).
		Str("metrics_port", cfg.MetricsPort).
		Bool("mgmt_enabled", cfg.EnableMgmt).
		Bool("metrics_enabled", cfg.EnableMetrics).
		Bool("reflection", cfg.EnableReflection).
		Bool("logging", cfg.EnableLogging).
		Msg("starting gRPC server")

	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", addr, err)
	}

	// Create recorder for e2e testing
	rec := recorder.New()

	// Create context-values store for pre-seeding request context
	contextValues := mm.NewStore()

	// Create metrics if enabled
	var metricsServer *metrics.Metrics
	if cfg.EnableMetrics {
		metricsServer = metrics.New(cfg.MetricsPort)
	}

	var opts []grpc.ServerOption
	// Always use recording interceptors for e2e testing
	opts = append(opts, grpc.UnaryInterceptor(recordingInterceptor(rec, metricsServer, cfg.EnableLogging, baseLogger, cfg.RequestIDHeaders, cfg.RequestIDResponseHeader, contextValues)))
	opts = append(opts, grpc.StreamInterceptor(streamingInterceptor(rec, metricsServer, cfg.EnableLogging, baseLogger, cfg.RequestIDHeaders, cfg.RequestIDResponseHeader, contextValues)))
	grpcServer := grpc.NewServer(opts...)

	if _, err := app.InitializeApp(grpcServer, cfg.EnableLogging); err != nil {
		return fmt.Errorf("failed to initialize app: %w", err)
	}

	if cfg.EnableReflection {
		reflection.Register(grpcServer)
	}

	// Start management server if enabled
	var mgmtServer *mgmt.Server
	if cfg.EnableMgmt {
		mgmtServer = mgmt.New(rec, cfg.MgmtPort, mgmt.WithContextValues(contextValues), mgmt.WithServiceInfo(grpcServer.GetServiceInfo))
		if err := mgmtServer.Start(); err != nil {
			return fmt.Errorf("failed to start management server: %w", err)
		}
	}

	// Start metrics server if enabled
	if metricsServer != nil {
		if err := metricsServer.Start(); err != nil {
			return fmt.Errorf("failed to start metrics server: %w", err)
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		if err := grpcServer.Serve(lis); err != nil {
			baseLogger.Fatal().Err(err).Msg("failed to serve")
		}
	}()

	<-ctx.Done()
	baseLogger.Info().Msg("shutdown signal received")

	grpcServer.GracefulStop()

	// Stop management server if it was started
	if mgmtServer != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := mgmtServer.Stop(shutdownCtx); err != nil {
			baseLogger.Error().Err(err).Msg("management server shutdown error")
		}
	}

	// Stop metrics server if it was started
	if metricsServer != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := metricsServer.Stop(shutdownCtx); err != nil {
			baseLogger.Error().Err(err).Msg("metrics server shutdown error")
		}
	}

	baseLogger.Info().Msg("server stopped")

	return nil
}

func recordingInterceptor(rec *recorder.Recorder, m *metrics.Metrics, enableLogging bool, baseLogger zerolog.Logger, requestIDHeaders string, requestIDResponseHeader string, contextValues *mm.Store) grpc.UnaryServerInterceptor {
	allowedHeaders := observability.NormalizeHeaderList(requestIDHeaders, observability.DefaultRequestIDHeaders)
	tracer := otel.Tracer("grpc-mock")

	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp any, err error) {
		startTime := time.Now()

		ctx, reqID, span, reqLogger := prepareContext(ctx, info.FullMethod, allowedHeaders, requestIDResponseHeader, contextValues, tracer, baseLogger)

		if m != nil {
			m.InFlightInc(info.FullMethod)
			defer m.InFlightDec(info.FullMethod)
		}

		if enableLogging {
			reqLogger.Info().Msg("gRPC request started")
		}

		record := recorder.CallRecord{
			RequestID: reqID,
			Method:    info.FullMethod,
			Timestamp: startTime,
			Request:   recorder.MarshalProto(req),
		}

		// Handle panics
		defer func() {
			if r := recover(); r != nil {
				record.DurationMs = time.Since(startTime).Milliseconds()
				record.Panic = fmt.Sprintf("%v", r)
				span.RecordError(fmt.Errorf("panic: %v", r))
				span.SetStatus(codes.Error, record.Panic)
				span.SetAttributes(attribute.String("rpc.status_code", "panic"))
				rec.Record(record)
				if enableLogging {
					reqLogger.Error().
						Int64("duration_ms", record.DurationMs).
						Str("status", "panic").
						Interface("panic", r).
						Msg("gRPC panic")
				}
				if m != nil {
					m.RecordRequest(info.FullMethod, record.DurationMs, "panic")
					m.RecordPanic(info.FullMethod, record.Panic)
				}
				err = fmt.Errorf("panic: %v", r)
				resp = nil
			}
		}()

		resp, err = handler(ctx, req)
		record.DurationMs = time.Since(startTime).Milliseconds()
		record.Response = recorder.MarshalProto(resp)

		finishRecord(ctx, rec, m, enableLogging, &record, err, span, reqLogger, info.FullMethod, startTime)

		rec.Record(record)
		return resp, err
	}
}

// streamingInterceptor records and meters streaming RPCs.
func streamingInterceptor(rec *recorder.Recorder, m *metrics.Metrics, enableLogging bool, baseLogger zerolog.Logger, requestIDHeaders string, requestIDResponseHeader string, contextValues *mm.Store) grpc.StreamServerInterceptor {
	allowedHeaders := observability.NormalizeHeaderList(requestIDHeaders, observability.DefaultRequestIDHeaders)
	tracer := otel.Tracer("grpc-mock")

	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) (err error) {
		startTime := time.Now()

		ctx, reqID, span, reqLogger := prepareContext(ss.Context(), info.FullMethod, allowedHeaders, requestIDResponseHeader, contextValues, tracer, baseLogger)

		// Wrap the stream so the context is enriched.
		wrapped := &contextStream{ServerStream: ss, ctx: ctx}

		if m != nil {
			m.InFlightInc(info.FullMethod)
			defer m.InFlightDec(info.FullMethod)
		}

		if enableLogging {
			reqLogger.Info().Str("stream_type", streamType(info)).Msg("gRPC stream started")
		}

		record := recorder.CallRecord{
			RequestID: reqID,
			Method:    info.FullMethod,
			Timestamp: startTime,
		}

		// Handle panics
		defer func() {
			if r := recover(); r != nil {
				record.DurationMs = time.Since(startTime).Milliseconds()
				record.Panic = fmt.Sprintf("%v", r)
				span.RecordError(fmt.Errorf("panic: %v", r))
				span.SetStatus(codes.Error, record.Panic)
				span.SetAttributes(attribute.String("rpc.status_code", "panic"))
				rec.Record(record)
				if enableLogging {
					reqLogger.Error().
						Int64("duration_ms", record.DurationMs).
						Str("status", "panic").
						Interface("panic", r).
						Msg("gRPC stream panic")
				}
				if m != nil {
					m.RecordRequest(info.FullMethod, record.DurationMs, "panic")
					m.RecordPanic(info.FullMethod, record.Panic)
				}
				err = fmt.Errorf("panic: %v", r)
			}
		}()

		err = handler(srv, wrapped)
		record.DurationMs = time.Since(startTime).Milliseconds()

		finishRecord(ctx, rec, m, enableLogging, &record, err, span, reqLogger, info.FullMethod, startTime)

		rec.Record(record)
		return err
	}
}

// prepareContext extracts trace context, resolves request-id, creates a span, and enriches the context.
func prepareContext(ctx context.Context, fullMethod string, allowedHeaders []string, requestIDResponseHeader string, contextValues *mm.Store, tracer trace.Tracer, baseLogger zerolog.Logger) (context.Context, string, trace.Span, zerolog.Logger) {
	// Extract propagated trace context from inbound gRPC metadata.
	if grpcMD, ok := metadata.FromIncomingContext(ctx); ok {
		ctx = otel.GetTextMapPropagator().Extract(ctx, &metadataCarrier{md: grpcMD})
	}

	// Start a server span named by the full gRPC method.
	ctx, span := tracer.Start(ctx, fullMethod,
		trace.WithSpanKind(trace.SpanKindServer),
		trace.WithAttributes(attribute.String("rpc.method", fullMethod)),
	)

	// Resolve request-id from inbound gRPC metadata (fallback to generated).
	var reqID string
	if grpcMD, ok := metadata.FromIncomingContext(ctx); ok {
		reqID = observability.ResolveRequestID(func(key string) string {
			if vals := grpcMD.Get(key); len(vals) > 0 {
				return vals[0]
			}
			return ""
		}, allowedHeaders)
	} else {
		reqID = observability.GenerateRequestID()
	}

	// Echo the request-id back in the response metadata.
	if requestIDResponseHeader != "" {
		_ = grpc.SetHeader(ctx, metadata.Pairs(requestIDResponseHeader, reqID))
	}

	// Extract trace-id from the span and enrich metadata + logger.
	var traceID string
	if sc := span.SpanContext(); sc.IsValid() {
		traceID = sc.TraceID().String()
	}

	// Store request metadata in context.
	reqMD := &observability.RequestMetadata{
		RequestID: reqID,
		TraceID:   traceID,
		Method:    fullMethod,
	}
	ctx = observability.WithRequestMetadata(ctx, reqMD)
	ctx = observability.WithRequestID(ctx, reqID)
	ctx = observability.WithMethod(ctx, fullMethod)
	ctx = observability.WithTraceID(ctx, traceID)

	// Derive a per-request logger with request_id, trace_id, and method fields.
	reqLogger := baseLogger.With().
		Str("request_id", reqID).
		Str("method", fullMethod).
		Logger()
	if traceID != "" {
		reqLogger = reqLogger.With().Str("trace_id", traceID).Logger()
	}
	ctx = observability.WithLogger(ctx, reqLogger)

	// Inject pre-seeded context values from the store into the request context.
	if seeded := contextValues.Get(reqID); len(seeded) > 0 {
		ctx = mm.WithValues(ctx, seeded)
	}

	return ctx, reqID, span, reqLogger
}

// finishRecord handles error/success logging, metrics, and span status for both unary and stream interceptors.
func finishRecord(_ context.Context, _ *recorder.Recorder, m *metrics.Metrics, enableLogging bool, record *recorder.CallRecord, err error, span trace.Span, reqLogger zerolog.Logger, fullMethod string, startTime time.Time) {
	if err != nil {
		record.Error = err.Error()
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		span.SetAttributes(attribute.String("rpc.status_code", "error"))
		if enableLogging {
			reqLogger.Error().
				Int64("duration_ms", record.DurationMs).
				Str("status", "error").
				Err(err).
				Msg("gRPC error")
		}
		if m != nil {
			m.RecordRequest(fullMethod, record.DurationMs, "error")
			m.RecordError(fullMethod, record.Error)
		}
	} else {
		span.SetAttributes(attribute.String("rpc.status_code", "ok"))
		if enableLogging {
			reqLogger.Info().
				Int64("duration_ms", record.DurationMs).
				Str("status", "success").
				Msg("gRPC success")
		}
		if m != nil {
			m.RecordRequest(fullMethod, record.DurationMs, "success")
		}
	}
}

// contextStream wraps grpc.ServerStream to override the context.
type contextStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *contextStream) Context() context.Context {
	return s.ctx
}

// streamType returns a human-readable description of the stream type.
func streamType(info *grpc.StreamServerInfo) string {
	switch {
	case info.IsClientStream && info.IsServerStream:
		return "bidi"
	case info.IsClientStream:
		return "client"
	case info.IsServerStream:
		return "server"
	default:
		return "unary"
	}
}

// metadataCarrier adapts gRPC metadata.MD to the otel TextMapCarrier interface.
type metadataCarrier struct {
	md metadata.MD
}

func (c *metadataCarrier) Get(key string) string {
	if vals := c.md.Get(key); len(vals) > 0 {
		return vals[0]
	}
	return ""
}

func (c *metadataCarrier) Set(key, value string) {
	c.md.Set(key, value)
}

func (c *metadataCarrier) Keys() []string {
	out := make([]string, 0, len(c.md))
	for k := range c.md {
		out = append(out, k)
	}
	return out
}
