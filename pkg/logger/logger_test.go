package logger

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/supporttools/node-doctor/pkg/types"
)

// jsonHandlerLogger builds an slog.Logger writing JSON to buf at the given level,
// without mutating global slog state.
func jsonLogger(buf *bytes.Buffer, level slog.Level) *slog.Logger {
	return slog.New(NewHandler(buf, "json", level))
}

func TestParseLevel(t *testing.T) {
	cases := map[string]slog.Level{
		"debug":   slog.LevelDebug,
		"INFO":    slog.LevelInfo,
		" warn ":  slog.LevelWarn,
		"warning": slog.LevelWarn,
		"error":   slog.LevelError,
		"fatal":   LevelFatal,
		"":        slog.LevelInfo,
		"bogus":   slog.LevelInfo,
	}
	for in, want := range cases {
		if got := ParseLevel(in); got != want {
			t.Errorf("ParseLevel(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestNewHandlerJSON(t *testing.T) {
	var buf bytes.Buffer
	l := jsonLogger(&buf, slog.LevelInfo)
	l.Info("hello world", "k", "v")

	var rec map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &rec); err != nil {
		t.Fatalf("output is not valid JSON: %v (%q)", err, buf.String())
	}
	if rec["msg"] != "hello world" {
		t.Errorf("msg = %v, want %q", rec["msg"], "hello world")
	}
	if rec["level"] != "INFO" {
		t.Errorf("level = %v, want INFO", rec["level"])
	}
	if rec["k"] != "v" {
		t.Errorf("attr k = %v, want v", rec["k"])
	}
}

func TestNewHandlerText(t *testing.T) {
	var buf bytes.Buffer
	l := slog.New(NewHandler(&buf, "text", slog.LevelInfo))
	l.Info("human readable")

	out := buf.String()
	// Text output is not JSON.
	var rec map[string]any
	if json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &rec) == nil {
		t.Errorf("text handler produced JSON: %q", out)
	}
	if !strings.Contains(out, "human readable") {
		t.Errorf("text output missing message: %q", out)
	}
	if !strings.Contains(out, "level=INFO") {
		t.Errorf("text output missing level: %q", out)
	}
}

func TestLevelFiltering(t *testing.T) {
	var buf bytes.Buffer
	l := jsonLogger(&buf, slog.LevelInfo)
	l.Debug("should be suppressed")
	if buf.Len() != 0 {
		t.Errorf("debug record emitted at info level: %q", buf.String())
	}
	l.Info("should appear")
	if !strings.Contains(buf.String(), "should appear") {
		t.Errorf("info record suppressed: %q", buf.String())
	}
}

func TestSplitPrefix(t *testing.T) {
	cases := []struct {
		in    string
		level slog.Level
		msg   string
	}{
		{"[ERROR] boom", slog.LevelError, "boom"},
		{"[error] lower", slog.LevelError, "lower"},
		{"[WARN] careful", slog.LevelWarn, "careful"},
		{"[WARNING] careful", slog.LevelWarn, "careful"},
		{"[INFO] note", slog.LevelInfo, "note"},
		{"[DEBUG] trace", slog.LevelDebug, "trace"},
		{"[FATAL] dying", LevelFatal, "dying"},
		{"no prefix here", slog.LevelInfo, "no prefix here"},
		{"[NOTALEVEL] keep", slog.LevelInfo, "[NOTALEVEL] keep"},
		{"  [INFO]  spaced  ", slog.LevelInfo, "spaced"},
	}
	for _, c := range cases {
		gotLevel, gotMsg := splitPrefix(c.in)
		if gotLevel != c.level || strings.TrimRight(gotMsg, " ") != c.msg {
			t.Errorf("splitPrefix(%q) = (%v, %q), want (%v, %q)", c.in, gotLevel, gotMsg, c.level, c.msg)
		}
	}
}

func TestBridgeWriter(t *testing.T) {
	var buf bytes.Buffer
	l := jsonLogger(&buf, slog.LevelDebug)
	bw := &bridgeWriter{logger: l}

	if _, err := bw.Write([]byte("[ERROR] boom\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	var rec map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &rec); err != nil {
		t.Fatalf("bridged output not JSON: %v (%q)", err, buf.String())
	}
	if rec["level"] != "ERROR" {
		t.Errorf("bridged level = %v, want ERROR", rec["level"])
	}
	if rec["msg"] != "boom" {
		t.Errorf("bridged msg = %v, want boom", rec["msg"])
	}

	// No-prefix line maps to info with message intact.
	buf.Reset()
	if _, err := bw.Write([]byte("plain line\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	rec = nil
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &rec); err != nil {
		t.Fatalf("bridged output not JSON: %v (%q)", err, buf.String())
	}
	if rec["level"] != "INFO" || rec["msg"] != "plain line" {
		t.Errorf("no-prefix bridge = (%v, %v), want (INFO, plain line)", rec["level"], rec["msg"])
	}
}

// withSavedGlobals saves and restores the global slog default and standard log
// package output/flags so Init tests don't leak state into other tests.
func withSavedGlobals(t *testing.T) {
	t.Helper()
	prevDefault := slog.Default()
	prevFlags := log.Flags()
	prevPrefix := log.Prefix()
	prevOut := log.Writer()
	t.Cleanup(func() {
		slog.SetDefault(prevDefault)
		log.SetFlags(prevFlags)
		log.SetPrefix(prevPrefix)
		log.SetOutput(prevOut)
	})
}

func TestInitFileOutput(t *testing.T) {
	withSavedGlobals(t)

	dir := t.TempDir()
	logPath := filepath.Join(dir, "nd.log")
	cfg := &types.NodeDoctorConfig{}
	cfg.Settings.LogLevel = "info"
	cfg.Settings.LogFormat = "json"
	cfg.Settings.LogOutput = "file"
	cfg.Settings.LogFile = logPath

	if err := Init(cfg); err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Native structured call and bridged stdlib call both go to the file.
	L().Info("structured record", "kind", "native")
	log.Printf("[WARN] bridged record")

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	if _, err := os.Stat(logPath); err != nil {
		t.Fatalf("log file missing: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) < 2 {
		t.Fatalf("expected >=2 log lines, got %d: %q", len(lines), string(data))
	}

	var sawNative, sawBridged bool
	for _, line := range lines {
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("log line not JSON: %v (%q)", err, line)
		}
		if rec["msg"] == "structured record" && rec["level"] == "INFO" {
			sawNative = true
		}
		if rec["msg"] == "bridged record" && rec["level"] == "WARN" {
			sawBridged = true
		}
	}
	if !sawNative {
		t.Errorf("native structured record not found in %q", string(data))
	}
	if !sawBridged {
		t.Errorf("bridged WARN record not found in %q", string(data))
	}
}

func TestInitFileOutputEmptyFileErrors(t *testing.T) {
	withSavedGlobals(t)

	cfg := &types.NodeDoctorConfig{}
	cfg.Settings.LogOutput = "file"
	cfg.Settings.LogFile = ""

	if err := Init(cfg); err == nil {
		t.Fatal("expected error for file output with empty logFile, got nil")
	}
}

func TestInitNilConfig(t *testing.T) {
	withSavedGlobals(t)
	if err := Init(nil); err == nil {
		t.Fatal("expected error for nil config")
	}
}

func TestBridgeWriterContextNotNil(t *testing.T) {
	// Guard against a nil context regression in Write.
	var buf bytes.Buffer
	bw := &bridgeWriter{logger: jsonLogger(&buf, slog.LevelInfo)}
	bw.logger.Log(context.Background(), slog.LevelInfo, "ctx ok")
	if !strings.Contains(buf.String(), "ctx ok") {
		t.Errorf("context log failed: %q", buf.String())
	}
}
