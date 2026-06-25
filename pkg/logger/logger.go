// Package logger wires structured logging (stdlib log/slog) from the validated
// Node Doctor configuration at startup.
//
// It serves two roles:
//
//  1. It configures slog's default logger from cfg.Settings (LogLevel,
//     LogFormat, LogOutput, LogFile) so code that wants real structured logging
//     can call logger.L() (or slog.Default()) and get key/value attributes.
//
//  2. It bridges the standard library `log` package onto slog so the large body
//     of existing `log.Printf("[LEVEL] ...")` call sites flow through the same
//     handler — honoring the configured format and destination — without having
//     to be rewritten. The bridge strips a leading "[INFO]"/"[WARN]"/"[ERROR]"/
//     "[DEBUG]" token (case-insensitive) from each line and maps it to the
//     corresponding slog level; lines with no recognized prefix log at info.
package logger

import (
	"context"
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"strings"

	"github.com/supporttools/node-doctor/pkg/types"
)

// LevelFatal is a synthetic level above Error used to map the "fatal" log level
// (a valid value in config) onto slog, which has no native fatal level. Records
// at this level are always emitted (it sits above Error).
const LevelFatal = slog.Level(12)

// L returns the active structured logger (slog's default). Code that wants to
// emit structured key/value attributes should call logger.L().Info(...) etc.
func L() *slog.Logger {
	return slog.Default()
}

// ParseLevel maps a config log level string (debug/info/warn/error/fatal) to a
// slog.Level. The match is case-insensitive and tolerant of surrounding
// whitespace. Unknown values fall back to slog.LevelInfo.
func ParseLevel(level string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	case "fatal":
		return LevelFatal
	default:
		return slog.LevelInfo
	}
}

// resolveWriter resolves the output writer for the configured LogOutput.
// Valid LogOutput values (see pkg/types/config.go) are "stdout", "stderr", and
// "file". For "file" it opens (creating if needed) cfg LogFile in append mode
// with 0644 permissions, returning a clear error if LogFile is empty or the
// file cannot be opened.
func resolveWriter(output, file string) (io.Writer, error) {
	switch strings.ToLower(strings.TrimSpace(output)) {
	case "", "stdout":
		return os.Stdout, nil
	case "stderr":
		return os.Stderr, nil
	case "file":
		if strings.TrimSpace(file) == "" {
			return nil, fmt.Errorf("logOutput is %q but logFile is empty", output)
		}
		f, err := os.OpenFile(file, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644) //nolint:gosec // log path comes from validated operator config
		if err != nil {
			return nil, fmt.Errorf("open log file %q: %w", file, err)
		}
		return f, nil
	default:
		return nil, fmt.Errorf("unsupported logOutput %q (want stdout, stderr or file)", output)
	}
}

// NewHandler builds a slog.Handler for the given format ("json" or "text"),
// writing to w at the given minimum level. Any unrecognized format defaults to
// JSON. It is exported so the construction can be unit-tested without mutating
// global slog/log state.
func NewHandler(w io.Writer, format string, level slog.Level) slog.Handler {
	opts := &slog.HandlerOptions{Level: level}
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "text":
		return slog.NewTextHandler(w, opts)
	default: // "json" and anything unexpected
		return slog.NewJSONHandler(w, opts)
	}
}

// Init configures structured logging from the validated configuration. It:
//
//   - resolves the output writer from cfg.Settings.LogOutput / LogFile,
//   - builds a JSON or text slog.Handler at the level mapped from LogLevel,
//   - installs it as slog's default logger, and
//   - redirects the standard `log` package through the prefix bridge so existing
//     log.Printf("[LEVEL] ...") calls honor the same format/destination.
//
// On error (e.g. an unopenable log file) Init returns the error WITHOUT mutating
// any global state, so the caller can warn-and-continue on the prior defaults.
func Init(cfg *types.NodeDoctorConfig) error {
	if cfg == nil {
		return fmt.Errorf("nil config")
	}
	w, err := resolveWriter(cfg.Settings.LogOutput, cfg.Settings.LogFile)
	if err != nil {
		return err
	}

	level := ParseLevel(cfg.Settings.LogLevel)
	handler := NewHandler(w, cfg.Settings.LogFormat, level)
	l := slog.New(handler)

	slog.SetDefault(l)

	// Bridge the standard log package onto slog. Clearing the flags prevents the
	// stdlib from prepending its own timestamp (slog adds its own time field).
	log.SetFlags(0)
	log.SetOutput(&bridgeWriter{logger: l})

	return nil
}

// bridgeWriter is an io.Writer adapter that routes every standard-library log
// write through slog. Per write it strips a recognized level prefix, trims the
// trailing newline, and emits the remainder at the mapped level.
type bridgeWriter struct {
	logger *slog.Logger
}

// Write implements io.Writer. The stdlib log package issues exactly one Write
// per log call (with a trailing newline), so we treat each write as one record.
func (b *bridgeWriter) Write(p []byte) (int, error) {
	n := len(p)
	msg := strings.TrimRight(string(p), "\n")
	level, msg := splitPrefix(msg)
	l := b.logger
	if l == nil {
		l = slog.Default()
	}
	l.Log(context.Background(), level, msg)
	return n, nil
}

// splitPrefix strips a leading, optionally bracketed level token from a log
// line and returns the mapped slog level plus the remaining message. The token
// is matched case-insensitively. Recognized tokens: DEBUG, INFO, WARN/WARNING,
// ERROR, FATAL. A line with no recognized prefix maps to info with the message
// returned unchanged.
func splitPrefix(line string) (slog.Level, string) {
	trimmed := strings.TrimLeft(line, " \t")
	if !strings.HasPrefix(trimmed, "[") {
		return slog.LevelInfo, line
	}
	end := strings.IndexByte(trimmed, ']')
	if end < 0 {
		return slog.LevelInfo, line
	}
	token := strings.ToUpper(strings.TrimSpace(trimmed[1:end]))
	var level slog.Level
	switch token {
	case "DEBUG":
		level = slog.LevelDebug
	case "INFO":
		level = slog.LevelInfo
	case "WARN", "WARNING":
		level = slog.LevelWarn
	case "ERROR":
		level = slog.LevelError
	case "FATAL":
		level = LevelFatal
	default:
		// Not a recognized level token; leave the line intact at info.
		return slog.LevelInfo, line
	}
	rest := strings.TrimLeft(trimmed[end+1:], " \t")
	return level, rest
}

// Ensure bridgeWriter satisfies io.Writer.
var _ io.Writer = (*bridgeWriter)(nil)
