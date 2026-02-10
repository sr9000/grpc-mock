package main

import (
	"context"
	"crypto/rand"
	"fmt"
	"grpc-mock/internal/app"
	"grpc-mock/pkg/ctxkeys"
	"grpc-mock/pkg/metrics"
	"grpc-mock/pkg/mgmt"
	"grpc-mock/pkg/recorder"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/caarlos0/env/v11"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

type Config struct {
	Host             string `env:"HOST" envDefault:"0.0.0.0"`
	Port             string `env:"PORT" envDefault:"50051"`
	MgmtPort         string `env:"MGMT_PORT" envDefault:"9000"`
	MetricsPort      string `env:"METRICS_PORT" envDefault:"9100"`
	EnableMgmt       bool   `env:"MGMT_ENABLED" envDefault:"true"`
	EnableMetrics    bool   `env:"METRICS_ENABLED" envDefault:"true"`
	EnableReflection bool   `env:"GRPC_REFLECTION" envDefault:"false"`
	EnableLogging    bool   `env:"GRPC_LOGGING" envDefault:"true"`
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
	runCmd.Flags().Bool("no-mgmt", false, "Disable management server (overrides MGMT_ENABLED env var)")
	runCmd.Flags().Bool("no-metrics", false, "Disable metrics server (overrides METRICS_ENABLED env var)")
	runCmd.Flags().BoolP("reflection", "r", false, "Enable gRPC server reflection (overrides GRPC_REFLECTION env var)")
	runCmd.Flags().Bool("no-logs", false, "Disable gRPC request logging (overrides GRPC_LOGGING env var)")

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
	if cmd.Flags().Changed("no-mgmt") {
		v, _ := cmd.Flags().GetBool("no-mgmt")
		cfg.EnableMgmt = !v
	}
	if cmd.Flags().Changed("no-metrics") {
		v, _ := cmd.Flags().GetBool("no-metrics")
		cfg.EnableMetrics = !v
	}
	if cmd.Flags().Changed("reflection") {
		v, _ := cmd.Flags().GetBool("reflection")
		cfg.EnableReflection = v
	}
	if cmd.Flags().Changed("no-logs") {
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

	addr := net.JoinHostPort(cfg.Host, cfg.Port)

	// Log effective configuration before starting.
	log.Printf("starting gRPC server with config: host=%s port=%s mgmt-port=%s metrics-port=%s mgmt-enabled=%t metrics-enabled=%t reflection=%t logging=%t",
		cfg.Host, cfg.Port, cfg.MgmtPort, cfg.MetricsPort, cfg.EnableMgmt, cfg.EnableMetrics, cfg.EnableReflection, cfg.EnableLogging)

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
	opts = append(opts, grpc.UnaryInterceptor(recordingInterceptor(rec, metricsServer, cfg.EnableLogging)))
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
			log.Fatalf("failed to serve: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("shutdown signal received")

	grpcServer.GracefulStop()

	// Stop management server if it was started
	if mgmtServer != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := mgmtServer.Stop(shutdownCtx); err != nil {
			log.Printf("management server shutdown error: %v", err)
		}
	}

	// Stop metrics server if it was started
	if metricsServer != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := metricsServer.Stop(shutdownCtx); err != nil {
			log.Printf("metrics server shutdown error: %v", err)
		}
	}

	log.Println("server stopped")

	return nil
}

func recordingInterceptor(rec *recorder.Recorder, m *metrics.Metrics, enableLogging bool) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp any, err error) {
		reqID := rand.Text()
		ctx = context.WithValue(ctx, ctxkeys.RequestID{}, reqID)
		startTime := time.Now()

		if enableLogging {
			log.Printf("[req_id=%s] gRPC request: %s", reqID, info.FullMethod)
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
					log.Printf("[req_id=%s] gRPC panic: %s: %v", reqID, info.FullMethod, r)
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
				log.Printf("[req_id=%s] gRPC error: %s: %v", reqID, info.FullMethod, err)
			}
			// Record metrics for error
			if m != nil {
				m.RecordRequest(info.FullMethod, record.DurationMs, "error")
				m.RecordError(info.FullMethod, record.Error)
			}
		} else {
			if enableLogging {
				log.Printf("[req_id=%s] gRPC success: %s", reqID, info.FullMethod)
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
