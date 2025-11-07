package remediators

import (
	"context"
	"fmt"
	"time"

	"github.com/supporttools/node-doctor/pkg/types"
)

// RemediateFunc is a function type that performs the actual remediation logic.
// It receives a context for cancellation support and the problem to remediate.
// It should return nil on success or an error describing the failure.
type RemediateFunc func(ctx context.Context, problem types.Problem) error

// Logger provides optional logging functionality for remediators.
// Remediators can use this interface to log their activities without
// requiring a specific logging implementation.
type Logger interface {
	// Infof logs an informational message with formatting
	Infof(format string, args ...interface{})

	// Warnf logs a warning message with formatting
	Warnf(format string, args ...interface{})

	// Errorf logs an error message with formatting
	Errorf(format string, args ...interface{})
}

// Common cooldown duration presets for different remediation types.
// These provide sensible defaults based on the impact and risk level.
const (
	// CooldownFast is for quick, low-risk remediations (e.g., DNS cache flush)
	CooldownFast = 3 * time.Minute

	// CooldownMedium is for standard service restarts (e.g., systemd services)
	CooldownMedium = 5 * time.Minute

	// CooldownSlow is for slow-starting services (e.g., database restarts)
	CooldownSlow = 10 * time.Minute

	// CooldownDestructive is for high-impact actions (e.g., node reboot)
	CooldownDestructive = 30 * time.Minute
)

// Default configuration values for remediators
const (
	// DefaultMaxAttempts is the default maximum number of remediation attempts
	// before giving up on a problem
	DefaultMaxAttempts = 3
)

// GenerateProblemKey generates a unique key for a problem based on its type and resource.
// This key is used for tracking cooldown periods and attempt counts per unique problem.
//
// The key format is: "type:resource"
// Examples:
//   - "kubelet-unhealthy:kubelet.service"
//   - "disk-pressure:/var/lib/docker"
//   - "memory-pressure:node"
func GenerateProblemKey(problem types.Problem) string {
	return fmt.Sprintf("%s:%s", problem.Type, problem.Resource)
}
