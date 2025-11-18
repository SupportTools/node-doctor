// Copyright 2025 Support Tools Contributors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

//go:build e2e
// +build e2e

package utils

import (
	"fmt"
	"os"
	"time"
)

// Log prints a timestamped log message to stdout
func Log(format string, args ...interface{}) {
	timestamp := time.Now().Format("15:04:05.000")
	message := fmt.Sprintf(format, args...)
	fmt.Printf("[%s] %s\n", timestamp, message)
}

// LogError prints an error message to stderr
func LogError(format string, args ...interface{}) {
	timestamp := time.Now().Format("15:04:05.000")
	message := fmt.Sprintf(format, args...)
	fmt.Fprintf(os.Stderr, "[%s] ERROR: %s\n", timestamp, message)
}

// LogDebug prints a debug message (only if E2E_DEBUG=1)
func LogDebug(format string, args ...interface{}) {
	if os.Getenv("E2E_DEBUG") != "1" {
		return
	}

	timestamp := time.Now().Format("15:04:05.000")
	message := fmt.Sprintf(format, args...)
	fmt.Printf("[%s] DEBUG: %s\n", timestamp, message)
}

// LogSection prints a section header for better readability
func LogSection(title string) {
	fmt.Println()
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Printf("  %s\n", title)
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println()
}

// LogSuccess prints a success message with checkmark
func LogSuccess(format string, args ...interface{}) {
	message := fmt.Sprintf(format, args...)
	timestamp := time.Now().Format("15:04:05.000")
	fmt.Printf("[%s] ✓ %s\n", timestamp, message)
}

// LogFailure prints a failure message with X mark
func LogFailure(format string, args ...interface{}) {
	message := fmt.Sprintf(format, args...)
	timestamp := time.Now().Format("15:04:05.000")
	fmt.Fprintf(os.Stderr, "[%s] ✗ %s\n", timestamp, message)
}

// LogWarning prints a warning message
func LogWarning(format string, args ...interface{}) {
	message := fmt.Sprintf(format, args...)
	timestamp := time.Now().Format("15:04:05.000")
	fmt.Printf("[%s] ⚠ %s\n", timestamp, message)
}
