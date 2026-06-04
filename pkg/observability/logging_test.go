package observability

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rs/zerolog"
)

func TestNewLoggerDefaultJSON(t *testing.T) {
	logger, closer, err := NewLogger(LogConfig{})
	if err != nil {
		t.Fatalf("NewLogger() error: %v", err)
	}
	if closer != nil {
		t.Error("expected nil closer for stdout output")
	}
	// Logger should be usable without panic
	logger.Info().Msg("test")
}

func TestNewLoggerLevelParsing(t *testing.T) {
	tests := []struct {
		level    string
		expected zerolog.Level
	}{
		{"debug", zerolog.DebugLevel},
		{"INFO", zerolog.InfoLevel},
		{"Warn", zerolog.WarnLevel},
		{"error", zerolog.ErrorLevel},
		{"", zerolog.InfoLevel},
	}
	for _, tt := range tests {
		t.Run(tt.level, func(t *testing.T) {
			logger, _, err := NewLogger(LogConfig{Level: tt.level})
			if err != nil {
				t.Fatalf("NewLogger(%q) error: %v", tt.level, err)
			}
			if logger.GetLevel() != tt.expected {
				t.Errorf("expected level %v, got %v", tt.expected, logger.GetLevel())
			}
		})
	}
}

func TestNewLoggerInvalidLevel(t *testing.T) {
	_, _, err := NewLogger(LogConfig{Level: "bogus"})
	if err == nil {
		t.Fatal("expected error for invalid level")
	}
	if !strings.Contains(err.Error(), "invalid log level") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestNewLoggerConsoleFormat(t *testing.T) {
	logger, _, err := NewLogger(LogConfig{Format: "console"})
	if err != nil {
		t.Fatalf("NewLogger(console) error: %v", err)
	}
	logger.Info().Msg("console test")
}

func TestNewLoggerFileOutput(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")

	logger, closer, err := NewLogger(LogConfig{Output: "file", File: path})
	if err != nil {
		t.Fatalf("NewLogger(file) error: %v", err)
	}
	if closer == nil {
		t.Fatal("expected non-nil closer for file output")
	}

	logger.Info().Str("key", "value").Msg("file test")
	if err := closer.Close(); err != nil {
		t.Fatalf("close error: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file error: %v", err)
	}
	if len(data) == 0 {
		t.Error("log file is empty")
	}
	if !strings.Contains(string(data), "file test") {
		t.Error("log file does not contain expected message")
	}
}

func TestNewLoggerFileOutputMissingPath(t *testing.T) {
	_, _, err := NewLogger(LogConfig{Output: "file"})
	if err == nil {
		t.Fatal("expected error when LOG_FILE is empty with file output")
	}
	if !strings.Contains(err.Error(), "LOG_FILE is required") {
		t.Errorf("unexpected error: %v", err)
	}
}
