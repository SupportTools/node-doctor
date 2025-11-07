// Package remediators provides the base functionality for implementing
// problem remediators in Node Doctor.
//
// The package defines the common infrastructure that all concrete remediators
// can use via embedding the BaseRemediator struct. This includes:
//   - Cooldown tracking per problem
//   - Attempt counting and limiting
//   - Thread-safe state management
//   - Optional logging
//   - Error handling and panic recovery
//
// Usage Example:
//
//	type SystemdRemediator struct {
//	    *remediators.BaseRemediator
//	    serviceName string
//	}
//
//	func NewSystemdRemediator(name, service string, cooldown time.Duration) (*SystemdRemediator, error) {
//	    base, err := remediators.NewBaseRemediator(name, cooldown)
//	    if err != nil {
//	        return nil, err
//	    }
//
//	    sr := &SystemdRemediator{
//	        BaseRemediator: base,
//	        serviceName:    service,
//	    }
//
//	    err = base.SetRemediateFunc(sr.restartService)
//	    if err != nil {
//	        return nil, err
//	    }
//
//	    return sr, nil
//	}
//
//	func (sr *SystemdRemediator) restartService(ctx context.Context, problem types.Problem) error {
//	    // Actual remediation logic
//	    return exec.CommandContext(ctx, "systemctl", "restart", sr.serviceName).Run()
//	}
//
// Thread Safety:
//
// BaseRemediator is designed to be thread-safe and can handle concurrent
// remediation requests for different problems. State tracking is protected
// by a read-write mutex.
//
// Cooldown Management:
//
// Cooldown is tracked per unique problem (based on Type and Resource).
// This prevents rapid repeated remediation attempts for the same issue
// while allowing remediation of different problems concurrently.
package remediators
