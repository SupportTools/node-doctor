package system

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/supporttools/node-doctor/pkg/types"
)

func newProbeMonitor(paths []string, prober func(string) error) *DiskMonitor {
	return &DiskMonitor{
		name:          "disk-test",
		config:        &DiskMonitorConfig{WriteProbePaths: paths, WriteProbeTimeout: 200 * time.Millisecond},
		probeInFlight: make(map[string]bool),
		writeProber:   prober,
	}
}

func readonlyCondition(t *testing.T, s *types.Status) types.Condition {
	t.Helper()
	var found *types.Condition
	for i := range s.Conditions {
		if s.Conditions[i].Type == "ReadonlyFilesystem" {
			if found != nil {
				t.Fatalf("more than one ReadonlyFilesystem condition: %+v", s.Conditions)
			}
			found = &s.Conditions[i]
		}
	}
	if found == nil {
		t.Fatalf("no ReadonlyFilesystem condition in %+v", s.Conditions)
	}
	return *found
}

func TestDefaultWriteProbeRealDir(t *testing.T) {
	dir := t.TempDir()
	if err := defaultWriteProbe(dir); err != nil {
		t.Fatalf("probe on a writable dir failed: %v", err)
	}
	left, _ := filepath.Glob(filepath.Join(dir, ".node-doctor-write-probe-*"))
	if len(left) != 0 {
		t.Errorf("probe left files behind: %v", left)
	}
	if err := defaultWriteProbe(filepath.Join(dir, "missing")); err == nil {
		t.Error("probe on a missing dir succeeded")
	}
}

func TestWriteProbeHealthy(t *testing.T) {
	m := newProbeMonitor([]string{"/a", "/b"}, func(string) error { return nil })
	s := types.NewStatus("disk-test")
	m.runWriteProbes(context.Background(), s)
	c := readonlyCondition(t, s)
	if c.Status != types.ConditionFalse || c.Reason != "WriteProbeOK" {
		t.Errorf("healthy probes: got %s/%s", c.Status, c.Reason)
	}
}

// The 2026-10-06 a1ubr720p02 failure: ext4 shut down after errors, mount flags unchanged,
// writes return EROFS/EIO.
func TestWriteProbeEIOFiresEvenWhenOtherPathIsHealthy(t *testing.T) {
	m := newProbeMonitor([]string{"/root-probe", "/scratch-probe"}, func(dir string) error {
		if dir == "/root-probe" {
			return &os.PathError{Op: "fsync", Path: dir, Err: syscall.EIO}
		}
		return nil
	})
	s := types.NewStatus("disk-test")
	m.runWriteProbes(context.Background(), s)
	c := readonlyCondition(t, s)
	if c.Status != types.ConditionTrue || c.Reason != "WriteProbeFailed" {
		t.Errorf("EIO on one path: got %s/%s, want True/WriteProbeFailed", c.Status, c.Reason)
	}
	if len(s.Events) != 1 || s.Events[0].Reason != "WriteProbeFailed" {
		t.Errorf("want one WriteProbeFailed event, got %+v", s.Events)
	}
}

// A device that dropped off the bus usually hangs writes instead of erroring.
func TestWriteProbeHangIsAFailureAndDoesNotStack(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	var calls atomic.Int32
	m := newProbeMonitor([]string{"/root-probe"}, func(string) error {
		calls.Add(1)
		<-release
		return nil
	})

	s := types.NewStatus("disk-test")
	m.runWriteProbes(context.Background(), s)
	if c := readonlyCondition(t, s); c.Status != types.ConditionTrue {
		t.Fatalf("hung probe: got %s, want True", c.Status)
	}

	s = types.NewStatus("disk-test")
	m.runWriteProbes(context.Background(), s)
	if c := readonlyCondition(t, s); c.Status != types.ConditionTrue {
		t.Fatalf("second check while probe still hung: got %s, want True", c.Status)
	}
	if n := calls.Load(); n != 1 {
		t.Errorf("a new probe was started while one was still blocked: %d calls", n)
	}
}

func TestWriteProbeDoesNotOverrideStatfsReadonly(t *testing.T) {
	m := newProbeMonitor([]string{"/a"}, func(string) error { return nil })
	s := types.NewStatus("disk-test")
	s.AddCondition(types.NewCondition("ReadonlyFilesystem", types.ConditionTrue, "ReadonlyFilesystem", "/ is ro"))
	m.runWriteProbes(context.Background(), s)
	for _, c := range s.Conditions {
		if c.Type == "ReadonlyFilesystem" && c.Status == types.ConditionFalse {
			t.Fatal("write probe reported False over the statfs read-only True")
		}
	}
}

func TestWriteProbeCancelledGivesNoVerdict(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	m := newProbeMonitor([]string{"/a"}, func(string) error { <-release; return errors.New("late") })
	m.config.WriteProbeTimeout = time.Hour
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s := types.NewStatus("disk-test")
	m.runWriteProbes(ctx, s)
	if len(s.Conditions) != 0 {
		t.Errorf("cancelled check produced a verdict: %+v", s.Conditions)
	}
}

func TestParseWriteProbeConfig(t *testing.T) {
	c, err := parseDiskConfig(map[string]interface{}{
		"writeProbePaths":   []interface{}{"/host-write-probe"},
		"writeProbeTimeout": "5s",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(c.WriteProbePaths) != 1 || c.WriteProbePaths[0] != "/host-write-probe" || c.WriteProbeTimeout != 5*time.Second {
		t.Errorf("parsed %+v", c)
	}
	if _, err := parseDiskConfig(map[string]interface{}{"writeProbePaths": "/x"}); err == nil {
		t.Error("string writeProbePaths accepted")
	}
	if _, err := parseDiskConfig(map[string]interface{}{"writeProbeTimeout": "soon"}); err == nil {
		t.Error("bad writeProbeTimeout accepted")
	}
	d := &DiskMonitorConfig{}
	if err := d.applyDefaults(); err != nil {
		t.Fatal(err)
	}
	if d.WriteProbeTimeout != 10*time.Second || len(d.WriteProbePaths) != 0 {
		t.Errorf("defaults: %+v", d)
	}
}
