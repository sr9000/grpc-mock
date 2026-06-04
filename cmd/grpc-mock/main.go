package main

import (
	"context"
	"fmt"
	"grpc-mock/internal/app"
	"grpc-mock/pkg/metrics"
	"grpc-mock/pkg/mgmt"
	"grpc-mock/pkg/observability"
	"grpc-mock/pkg/recorder"
	"io"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/caarlos0/env/v11"
	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/reflection"
)

type Config struct {
	Host                    string `env:"HOST" envDefault:"0.0.0.0"`
	Port                    string `env:"PORT" envDefault:"50051"`
	MgmtPort                string `env:"MGMT_PORT" envDefault:"9000"`
	MetricsPort             string `env:"METRICS_PORT" envDefault:"9100"`
	EnableMgmt              bool   `env:"MGMT_ENABLED" envDefault:"true"`
	EnableMetrics           bool   `env:"METRICS_ENABLED" envDefault:"true"`
	EnableReflection        bool   `env:"GRPC_REFLECTION" envDefault:"false"`
	EnableLogging           bool   `env:"GRPC_LOGGING" envDefault:"true"`
	LogFormat               string `env:"LOG_FORMAT" envDefault:"json"`
	LogOutput               string `env:"LOG_OUTPUT" envDefault:"stdout"`
	LogFile                 string `env:"LOG_FILE"`
	LogLevel                string `env:"LOG_LEVEL" envDefault:"info"`
	RequestIDHeaders        string `env:"REQUEST_ID_HEADERS"`
	RequestIDResponseHeader string `env:"REQUEST_ID_RESPONSE_HEADER" envDefault:"x-request-id"`
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

	// Create metrics if enabled
	var metricsServer *metrics.Metrics
	if cfg.EnableMetrics {
		metricsServer = metrics.New(cfg.MetricsPort)
	}

	var opts []grpc.ServerOption
	// Always use recording interceptor for e2e testing
	opts = append(opts, grpc.UnaryInterceptor(recordingInterceptor(rec, metricsServer, cfg.EnableLogging, baseLogger, cfg.RequestIDHeaders, cfg.RequestIDResponseHeader)))
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
		mgmtServer = mgmt.New(rec, cfg.MgmtPort)
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

func recordingInterceptor(rec *recorder.Recorder, m *metrics.Metrics, enableLogging bool, baseLogger zerolog.Logger, requestIDHeaders string, requestIDResponseHeader string) grpc.UnaryServerInterceptor {
	allowedHeaders := observability.NormalizeHeaderList(requestIDHeaders, observability.DefaultRequestIDHeaders)

	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp any, err error) {
		startTime := time.Now()

		// Resolve request-id from inbound gRPC metadata (fallback to generated).
		var reqID string
		if md, ok := metadata.FromIncomingContext(ctx); ok {
			reqID = observability.ResolveRequestID(func(key string) string {
				if vals := md.Get(key); len(vals) > 0 {
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

		// Store request metadata in context.
		md := &observability.RequestMetadata{
			RequestID: reqID,
			Method:    info.FullMethod,
		}
		ctx = observability.WithRequestMetadata(ctx, md)
		ctx = observability.WithRequestID(ctx, reqID)
		ctx = observability.WithMethod(ctx, info.FullMethod)

		// Derive a per-request logger with request_id and method fields.
		reqLogger := baseLogger.With().
			Str("request_id", reqID).
			Str("method", info.FullMethod).
			Logger()
		ctx = observability.WithLogger(ctx, reqLogger)

		if enableLogging {
			reqLogger.Info().Msg("gRPC request started")
		}

		record := recorder.CallRecord{
			RequestID: reqID,
			Method:    info.FullMethod,
			Timestamp: startTime,
			Request:   req,
		}

		// Handle panics
		defer func() {
			if r := recover(); r != nil {
				record.DurationMs = time.Since(startTime).Milliseconds()
				record.Panic = fmt.Sprintf("%v", r)
				rec.Record(record)
				if enableLogging {
					reqLogger.Error().
						Int64("duration_ms", record.DurationMs).
						Str("status", "panic").
						Interface("panic", r).
						Msg("gRPC panic")
				}
				// Record metrics for panic
				if m != nil {
					m.RecordRequest(info.FullMethod, record.DurationMs, "panic")
					m.RecordPanic(info.FullMethod, record.Panic)
				}

				// Wrap panic as error to return to client
				err = fmt.Errorf("panic: %v", r)
				resp = nil
			}
		}()

		resp, err = handler(ctx, req)
		record.DurationMs = time.Since(startTime).Milliseconds()
		record.Response = resp

		if err != nil {
			record.Error = err.Error()
			if enableLogging {
				reqLogger.Error().
					Int64("duration_ms", record.DurationMs).
					Str("status", "error").
					Err(err).
					Msg("gRPC error")
			}
			// Record metrics for error
			if m != nil {
				m.RecordRequest(info.FullMethod, record.DurationMs, "error")
				m.RecordError(info.FullMethod, record.Error)
			}
		} else {
			if enableLogging {
				reqLogger.Info().
					Int64("duration_ms", record.DurationMs).
					Str("status", "success").
					Msg("gRPC success")
			}
			// Record request metrics for success
			if m != nil {
				m.RecordRequest(info.FullMethod, record.DurationMs, "success")
			}
		}

		rec.Record(record)
		return resp, err
	}
}

// Silence unused import guard.
var _ io.Closer = (io.Closer)(nil)
