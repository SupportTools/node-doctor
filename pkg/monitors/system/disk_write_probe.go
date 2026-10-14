package system

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/supporttools/node-doctor/pkg/types"
)

// defaultWriteProbe creates a file in dir, writes to it, fsyncs it and removes it. Any error
// on the way means the filesystem under dir is not taking writes.
func defaultWriteProbe(dir string) error {
	f, err := os.CreateTemp(dir, ".node-doctor-write-probe-*")
	if err != nil {
		return fmt.Errorf("create: %w", err)
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err := f.Write([]byte(time.Now().UTC().Format(time.RFC3339Nano))); err != nil {
		_ = f.Close() // the write error is the verdict; a close error adds nothing
		return fmt.Errorf("write: %w", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close() // the fsync error is the verdict; a close error adds nothing
		return fmt.Errorf("fsync: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close: %w", err)
	}
	if err := os.Remove(name); err != nil {
		return fmt.Errorf("unlink: %w", err)
	}
	return nil
}

// runWriteProbes probes every configured path and reports ONE ReadonlyFilesystem condition
// (written to the node as NodeDoctorReadonlyFilesystem): True if any probe failed, False if
// all passed. It never reports False over a True already set by the statfs read-only check.
func (d *DiskMonitor) runWriteProbes(ctx context.Context, status *types.Status) {
	if len(d.config.WriteProbePaths) == 0 {
		return
	}
	var failed []string
	verdicts := 0
	for _, dir := range d.config.WriteProbePaths {
		verdict, err := d.writeProbe(ctx, dir)
		if !verdict {
			continue
		}
		verdicts++
		if err != nil {
			msg := fmt.Sprintf("Write probe on %s failed: %v", dir, err)
			status.AddEvent(types.NewEvent(types.EventError, "WriteProbeFailed", msg+
				". The filesystem is not taking writes (read-only, shut down after errors, or its device is gone)"))
			failed = append(failed, msg)
		}
	}
	if len(failed) > 0 {
		status.AddCondition(types.NewCondition(
			"ReadonlyFilesystem",
			types.ConditionTrue,
			"WriteProbeFailed",
			strings.Join(failed, "; "),
		))
		return
	}
	if verdicts == 0 {
		return
	}
	for _, c := range status.Conditions {
		if c.Type == "ReadonlyFilesystem" && c.Status == types.ConditionTrue {
			return
		}
	}
	status.AddCondition(types.NewCondition(
		"ReadonlyFilesystem",
		types.ConditionFalse,
		"WriteProbeOK",
		fmt.Sprintf("Write probes succeeded on %s (create, write, fsync, unlink)", strings.Join(d.config.WriteProbePaths, ", ")),
	))
}

// writeProbe runs one bounded probe against dir. verdict is false when the check was
// cancelled before the probe finished, in which case err means nothing.
//
// A probe still running from an earlier check (a write hung on a dead device) is itself
// the failure: it is reported without starting another, so a wedged filesystem never
// accumulates blocked goroutines.
func (d *DiskMonitor) writeProbe(ctx context.Context, dir string) (verdict bool, err error) {
	d.probeMu.Lock()
	if d.probeInFlight[dir] {
		d.probeMu.Unlock()
		return true, fmt.Errorf("previous write probe still blocked")
	}
	d.probeInFlight[dir] = true
	d.probeMu.Unlock()

	done := make(chan error, 1)
	go func() {
		perr := d.writeProber(dir)
		d.probeMu.Lock()
		d.probeInFlight[dir] = false
		d.probeMu.Unlock()
		done <- perr
	}()

	timer := time.NewTimer(d.config.WriteProbeTimeout)
	defer timer.Stop()

	select {
	case perr := <-done:
		return true, perr
	case <-timer.C:
		return true, fmt.Errorf("timed out after %s", d.config.WriteProbeTimeout)
	case <-ctx.Done():
		return false, nil
	}
}
