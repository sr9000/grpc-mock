package main

import (
	"context"
	"crypto/rand"
	"fmt"
	"grpc-mock/internal/app"
	"grpc-mock/pkg/ctxkeys"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/caarlos0/env/v11"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

type Config struct {
	Host             string `env:"HOST" envDefault:"0.0.0.0"`
	Port             string `env:"PORT" envDefault:"50051"`
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
	log.Printf("starting gRPC server with config: host=%s port=%s reflection=%t logging=%t",
		cfg.Host, cfg.Port, cfg.EnableReflection, cfg.EnableLogging)

	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", addr, err)
	}

	var opts []grpc.ServerOption
	if cfg.EnableLogging {
		opts = append(opts, grpc.UnaryInterceptor(loggingInterceptor))
	}
	grpcServer := grpc.NewServer(opts...)

	if _, err := app.InitializeApp(grpcServer, cfg.EnableLogging); err != nil {
		return fmt.Errorf("failed to initialize app: %w", err)
	}

	if cfg.EnableReflection {
		reflection.Register(grpcServer)
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
	log.Println("server stopped")

	return nil
}

func loggingInterceptor(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	reqID := rand.Text()
	ctx = context.WithValue(ctx, ctxkeys.RequestID{}, reqID)
	log.Printf("[req_id=%s] gRPC request: %s", reqID, info.FullMethod)

	resp, err := handler(ctx, req)
	if err != nil {
		log.Printf("[req_id=%s] gRPC error: %s: %v", reqID, info.FullMethod, err)
	} else {
		log.Printf("[req_id=%s] gRPC success: %s", reqID, info.FullMethod)
	}

	return resp, err
}
