package logger

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sirupsen/logrus"
)

func TestInitialize(t *testing.T) {
	tests := []struct {
		name       string
		level      string
		format     string
		output     string
		outputFile string
		wantErr    bool
	}{
		{
			name:    "valid json stdout debug",
			level:   "debug",
			format:  "json",
			output:  "stdout",
			wantErr: false,
		},
		{
			name:    "valid text stderr info",
			level:   "info",
			format:  "text",
			output:  "stderr",
			wantErr: false,
		},
		{
			name:    "valid text stdout warn",
			level:   "warn",
			format:  "text",
			output:  "stdout",
			wantErr: false,
		},
		{
			name:    "valid json stdout error",
			level:   "error",
			format:  "json",
			output:  "stdout",
			wantErr: false,
		},
		{
			name:    "invalid log level",
			level:   "invalid",
			format:  "json",
			output:  "stdout",
			wantErr: true,
		},
		{
			name:    "invalid format",
			level:   "info",
			format:  "invalid",
			output:  "stdout",
			wantErr: true,
		},
		{
			name:    "invalid output",
			level:   "info",
			format:  "json",
			output:  "invalid",
			wantErr: true,
		},
		{
			name:       "file output missing file path",
			level:      "info",
			format:     "json",
			output:     "file",
			outputFile: "",
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Initialize(tt.level, tt.format, tt.output, tt.outputFile)
			if (err != nil) != tt.wantErr {
				t.Errorf("Initialize() error = %v, wantErr %v", err, tt.wantErr)
			}

			// Verify level was set correctly if no error expected
			if !tt.wantErr {
				expectedLevel, _ := logrus.ParseLevel(tt.level)
				if log.GetLevel() != expectedLevel {
					t.Errorf("Expected log level %v, got %v", expectedLevel, log.GetLevel())
				}
			}
		})
	}
}

func TestInitializeWithFile(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "test.log")

	err := Initialize("info", "json", "file", logFile)
	if err != nil {
		t.Fatalf("Failed to initialize with file: %v", err)
	}

	// Write a log message
	Info("test message")

	// Flush and close to ensure data is written
	if err := Flush(); err != nil {
		t.Fatalf("Failed to flush: %v", err)
	}
	if err := Close(); err != nil {
		t.Fatalf("Failed to close: %v", err)
	}

	// Verify file was created and contains data
	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("Failed to read log file: %v", err)
	}

	if len(data) == 0 {
		t.Error("Log file is empty")
	}

	// Verify it's valid JSON
	var logEntry map[string]interface{}
	if err := json.Unmarshal(data, &logEntry); err != nil {
		t.Errorf("Log output is not valid JSON: %v", err)
	}
}

func TestLogLevels(t *testing.T) {
	tests := []struct {
		name          string
		logLevel      string
		logFunc       func(...interface{})
		shouldAppear  bool
	}{
		{"debug at debug level", "debug", Debug, true},
		{"info at debug level", "debug", Info, true},
		{"warn at debug level", "debug", Warn, true},
		{"error at debug level", "debug", Error, true},

		{"debug at info level", "info", Debug, false},
		{"info at info level", "info", Info, true},
		{"warn at info level", "info", Warn, true},
		{"error at info level", "info", Error, true},

		{"debug at warn level", "warn", Debug, false},
		{"info at warn level", "warn", Info, false},
		{"warn at warn level", "warn", Warn, true},
		{"error at warn level", "warn", Error, true},

		{"debug at error level", "error", Debug, false},
		{"info at error level", "error", Info, false},
		{"warn at error level", "error", Warn, false},
		{"error at error level", "error", Error, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			log.SetOutput(&buf)
			log.SetFormatter(&logrus.TextFormatter{
				DisableTimestamp: true,
			})

			level, _ := logrus.ParseLevel(tt.logLevel)
			log.SetLevel(level)

			tt.logFunc("test message")

			output := buf.String()
			if tt.shouldAppear && output == "" {
				t.Error("Expected log output but got none")
			}
			if !tt.shouldAppear && output != "" {
				t.Errorf("Expected no log output but got: %s", output)
			}
		})
	}
}

func TestJSONFormat(t *testing.T) {
	var buf bytes.Buffer
	err := Initialize("info", "json", "stdout", "")
	if err != nil {
		t.Fatalf("Failed to initialize: %v", err)
	}

	log.SetOutput(&buf)

	Info("test message")

	// Verify it's valid JSON
	var logEntry map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &logEntry); err != nil {
		t.Errorf("Log output is not valid JSON: %v\nOutput: %s", err, buf.String())
	}

	// Verify required fields
	if logEntry["msg"] != "test message" {
		t.Errorf("Expected msg='test message', got %v", logEntry["msg"])
	}
	if logEntry["level"] != "info" {
		t.Errorf("Expected level='info', got %v", logEntry["level"])
	}
	if _, ok := logEntry["time"]; !ok {
		t.Error("Expected 'time' field in JSON output")
	}
}

func TestTextFormat(t *testing.T) {
	var buf bytes.Buffer
	err := Initialize("info", "text", "stdout", "")
	if err != nil {
		t.Fatalf("Failed to initialize: %v", err)
	}

	log.SetOutput(&buf)

	Info("test message")

	output := buf.String()
	if !strings.Contains(output, "test message") {
		t.Errorf("Expected output to contain 'test message', got: %s", output)
	}
	if !strings.Contains(output, "level=info") {
		t.Errorf("Expected output to contain 'level=info', got: %s", output)
	}
}

func TestWithFields(t *testing.T) {
	var buf bytes.Buffer
	err := Initialize("info", "json", "stdout", "")
	if err != nil {
		t.Fatalf("Failed to initialize: %v", err)
	}

	log.SetOutput(&buf)

	WithFields(logrus.Fields{
		"component": "detector",
		"monitor":   "cpu-monitor",
	}).Info("test message")

	var logEntry map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &logEntry); err != nil {
		t.Fatalf("Failed to parse JSON: %v", err)
	}

	if logEntry["component"] != "detector" {
		t.Errorf("Expected component='detector', got %v", logEntry["component"])
	}
	if logEntry["monitor"] != "cpu-monitor" {
		t.Errorf("Expected monitor='cpu-monitor', got %v", logEntry["monitor"])
	}
	if logEntry["msg"] != "test message" {
		t.Errorf("Expected msg='test message', got %v", logEntry["msg"])
	}
}

func TestWithField(t *testing.T) {
	var buf bytes.Buffer
	err := Initialize("info", "json", "stdout", "")
	if err != nil {
		t.Fatalf("Failed to initialize: %v", err)
	}

	log.SetOutput(&buf)

	WithField("component", "exporter").Info("test message")

	var logEntry map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &logEntry); err != nil {
		t.Fatalf("Failed to parse JSON: %v", err)
	}

	if logEntry["component"] != "exporter" {
		t.Errorf("Expected component='exporter', got %v", logEntry["component"])
	}
}

func TestWithError(t *testing.T) {
	var buf bytes.Buffer
	err := Initialize("info", "json", "stdout", "")
	if err != nil {
		t.Fatalf("Failed to initialize: %v", err)
	}

	log.SetOutput(&buf)

	testErr := os.ErrNotExist
	WithError(testErr).Error("operation failed")

	var logEntry map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &logEntry); err != nil {
		t.Fatalf("Failed to parse JSON: %v", err)
	}

	if logEntry["error"] == nil {
		t.Error("Expected 'error' field in log entry")
	}
	if logEntry["msg"] != "operation failed" {
		t.Errorf("Expected msg='operation failed', got %v", logEntry["msg"])
	}
}

func TestFormattedLogging(t *testing.T) {
	var buf bytes.Buffer
	err := Initialize("info", "json", "stdout", "")
	if err != nil {
		t.Fatalf("Failed to initialize: %v", err)
	}

	log.SetOutput(&buf)

	Infof("formatted message: %s = %d", "count", 42)

	var logEntry map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &logEntry); err != nil {
		t.Fatalf("Failed to parse JSON: %v", err)
	}

	if logEntry["msg"] != "formatted message: count = 42" {
		t.Errorf("Expected formatted message, got %v", logEntry["msg"])
	}
}

func TestGet(t *testing.T) {
	logger := Get()
	if logger == nil {
		t.Error("Get() returned nil logger")
	}
	if logger != log {
		t.Error("Get() returned different logger instance")
	}
}

func TestSetAndGetLevel(t *testing.T) {
	SetLevel(logrus.DebugLevel)
	if GetLevel() != logrus.DebugLevel {
		t.Errorf("Expected level DebugLevel, got %v", GetLevel())
	}

	SetLevel(logrus.WarnLevel)
	if GetLevel() != logrus.WarnLevel {
		t.Errorf("Expected level WarnLevel, got %v", GetLevel())
	}
}
