// Package logger provides structured logging for Node Doctor using Logrus.
// It supports both JSON and text formats, multiple log levels, and structured field logging.
package logger

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/sirupsen/logrus"
)

// Global logger instance
var (
	log            *logrus.Logger
	mu             sync.RWMutex
	currentLogFile io.Closer // Track file handle for cleanup
)

// init creates a default logger instance
func init() {
	log = logrus.New()
	log.SetLevel(logrus.InfoLevel)
	log.SetFormatter(&logrus.TextFormatter{
		FullTimestamp: true,
	})
	log.SetOutput(os.Stdout)
}

// Initialize sets up the global logger with the specified configuration.
// This function is thread-safe and can be called multiple times.
// Parameters:
//   - level: Log level (debug, info, warn, error)
//   - format: Output format (json, text)
//   - output: Output destination (stdout, stderr, file)
//   - outputFile: File path when output is "file"
func Initialize(level, format, output string, outputFile string) error {
	mu.Lock()
	defer mu.Unlock()

	// Close existing file if re-initializing
	if currentLogFile != nil {
		if err := currentLogFile.Close(); err != nil {
			// Log to stderr since logger might not be functional
			fmt.Fprintf(os.Stderr, "Warning: failed to close previous log file: %v\n", err)
		}
		currentLogFile = nil
	}

	// Create new logger instance
	log = logrus.New()

	// Set level with validation
	lvl, err := logrus.ParseLevel(level)
	if err != nil {
		return fmt.Errorf("invalid log level %q: %w", level, err)
	}
	log.SetLevel(lvl)

	// Set format
	switch format {
	case "json":
		log.SetFormatter(&logrus.JSONFormatter{
			TimestampFormat: "2006-01-02T15:04:05.000Z07:00",
		})
	case "text":
		log.SetFormatter(&logrus.TextFormatter{
			FullTimestamp:   true,
			TimestampFormat: "2006-01-02 15:04:05",
		})
	default:
		return fmt.Errorf("invalid log format %q: must be json or text", format)
	}

	// Set output with buffering for file output
	var writer io.Writer
	switch output {
	case "stdout":
		writer = os.Stdout
	case "stderr":
		writer = os.Stderr
	case "file":
		if outputFile == "" {
			return fmt.Errorf("logFile must be specified when logOutput is 'file'")
		}
		file, err := os.OpenFile(outputFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return fmt.Errorf("failed to open log file %q: %w", outputFile, err)
		}

		// Add buffering to reduce I/O blocking
		bufferedWriter := bufio.NewWriterSize(file, 256*1024) // 256KB buffer
		currentLogFile = &bufferedFileWriter{
			Writer: bufferedWriter,
			file:   file,
		}
		writer = bufferedWriter
	default:
		return fmt.Errorf("invalid log output %q: must be stdout, stderr, or file", output)
	}
	log.SetOutput(writer)

	return nil
}

// bufferedFileWriter wraps a buffered writer and file for proper cleanup
type bufferedFileWriter struct {
	*bufio.Writer
	file *os.File
}

// Close flushes the buffer and closes the file
func (w *bufferedFileWriter) Close() error {
	if err := w.Flush(); err != nil {
		w.file.Close() // Still try to close file
		return fmt.Errorf("failed to flush log buffer: %w", err)
	}
	return w.file.Close()
}

// Get returns the global logger instance
func Get() *logrus.Logger {
	return log
}

// WithFields returns a logger entry with structured fields.
// Use this to add context to log messages:
//
//	logger.WithFields(logrus.Fields{
//	    "component": "detector",
//	    "monitor": "cpu-monitor",
//	}).Info("Monitor started")
func WithFields(fields logrus.Fields) *logrus.Entry {
	return log.WithFields(fields)
}

// WithField returns a logger entry with a single structured field
func WithField(key string, value interface{}) *logrus.Entry {
	return log.WithField(key, value)
}

// WithError returns a logger entry with an error field
func WithError(err error) *logrus.Entry {
	return log.WithError(err)
}

// Helper functions for direct logging

// Debug logs a message at level Debug
func Debug(args ...interface{}) {
	log.Debug(args...)
}

// Info logs a message at level Info
func Info(args ...interface{}) {
	log.Info(args...)
}

// Warn logs a message at level Warn
func Warn(args ...interface{}) {
	log.Warn(args...)
}

// Error logs a message at level Error
func Error(args ...interface{}) {
	log.Error(args...)
}

// Fatal logs a message at level Fatal then calls os.Exit(1)
func Fatal(args ...interface{}) {
	log.Fatal(args...)
}

// Debugf logs a formatted message at level Debug
func Debugf(format string, args ...interface{}) {
	log.Debugf(format, args...)
}

// Infof logs a formatted message at level Info
func Infof(format string, args ...interface{}) {
	log.Infof(format, args...)
}

// Warnf logs a formatted message at level Warn
func Warnf(format string, args ...interface{}) {
	log.Warnf(format, args...)
}

// Errorf logs a formatted message at level Error
func Errorf(format string, args ...interface{}) {
	log.Errorf(format, args...)
}

// Fatalf logs a formatted message at level Fatal then calls os.Exit(1)
func Fatalf(format string, args ...interface{}) {
	log.Fatalf(format, args...)
}

// SetLevel sets the log level programmatically
func SetLevel(level logrus.Level) {
	log.SetLevel(level)
}

// GetLevel returns the current log level
func GetLevel() logrus.Level {
	return log.GetLevel()
}

// Close flushes any buffered log data and closes the log file if one is open.
// This function is thread-safe and should be called during application shutdown.
// It's safe to call Close() multiple times.
func Close() error {
	mu.Lock()
	defer mu.Unlock()

	if currentLogFile != nil {
		err := currentLogFile.Close()
		currentLogFile = nil
		return err
	}
	return nil
}

// Flush ensures all buffered log data is written to the output.
// This is important to call before application shutdown to ensure
// no log messages are lost.
func Flush() error {
	mu.RLock()
	defer mu.RUnlock()

	if flusher, ok := log.Out.(interface{ Flush() error }); ok {
		return flusher.Flush()
	}
	return nil
}
