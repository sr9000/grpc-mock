package observability

import (
	"context"
	"os"
	"testing"
)

func TestSetupTracingDisabled(t *testing.T) {
	shutdown, err := SetupTracing(context.Background(), TraceConfig{
		Enabled:  false,
		Exporter: "none",
	})
	if err != nil {
		t.Fatalf("SetupTracing(disabled) error: %v", err)
	}
	if err := shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown error: %v", err)
	}
}

func TestSetupTracingNoneExporter(t *testing.T) {
	shutdown, err := SetupTracing(context.Background(), TraceConfig{
		Enabled:  true,
		Exporter: "none",
	})
	if err != nil {
		t.Fatalf("SetupTracing(none) error: %v", err)
	}
	if err := shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown error: %v", err)
	}
}

func TestSetupTracingFileExporter(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/traces.json"

	shutdown, err := SetupTracing(context.Background(), TraceConfig{
		Enabled:       true,
		Exporter:      "file",
		File:          path,
		SamplingRatio: 1.0,
		ServiceName:   "test",
	})
	if err != nil {
		t.Fatalf("SetupTracing(file) error: %v", err)
	}
	if err := shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown error: %v", err)
	}
}

func TestSetupTracingUnsupportedExporter(t *testing.T) {
	_, err := SetupTracing(context.Background(), TraceConfig{
		Enabled:  true,
		Exporter: "bogus",
	})
	if err == nil {
		t.Fatal("expected error for unsupported exporter")
	}
}

func TestSetupTracingFileExporterMissingPath(t *testing.T) {
	// File exporter with empty path should use default "./traces.json"
	shutdown, err := SetupTracing(context.Background(), TraceConfig{
		Enabled:     true,
		Exporter:    "file",
		File:        "",
		ServiceName: "test",
	})
	if err != nil {
		t.Fatalf("SetupTracing(file, empty path) error: %v", err)
	}
	if err := shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown error: %v", err)
	}
	// Clean up the default trace file
	_ = os.Remove("./traces.json")
}
