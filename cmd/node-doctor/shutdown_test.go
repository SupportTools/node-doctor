package main

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/supporttools/node-doctor/pkg/detector"
	"github.com/supporttools/node-doctor/pkg/types"
)

type stopRecorder struct {
	mu    sync.Mutex
	order []string
}

func (r *stopRecorder) record(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.order = append(r.order, name)
}

func (r *stopRecorder) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.order...)
}

type chattyMonitor struct {
	rec  *stopRecorder
	stop chan struct{}
}

func (m *chattyMonitor) Start() (<-chan *types.Status, error) {
	out := make(chan *types.Status)
	go func() {
		for {
			select {
			case <-m.stop:
				return
			case out <- types.NewStatus("chatty"):
			}
		}
	}()
	return out, nil
}

func (m *chattyMonitor) Stop() {
	m.rec.record("monitor")
	close(m.stop)
}

type recordingExporter struct {
	rec             *stopRecorder
	mu              sync.Mutex
	stopped         bool
	exportAfterStop bool
}

func (e *recordingExporter) ExportStatus(context.Context, *types.Status) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.stopped {
		e.exportAfterStop = true
	}
	return nil
}

func (e *recordingExporter) ExportProblem(context.Context, *types.Problem) error { return nil }

func (e *recordingExporter) Stop() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.stopped = true
	e.rec.record("exporter")
	return nil
}

func (e *recordingExporter) exportedAfterStop() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.exportAfterStop
}

func TestShutdown_StopsMonitorsBeforeExporters(t *testing.T) {
	rec := &stopRecorder{}
	mon := &chattyMonitor{rec: rec, stop: make(chan struct{})}
	exp := &recordingExporter{rec: rec}

	cfg := &types.NodeDoctorConfig{
		APIVersion: "v1",
		Kind:       "NodeDoctorConfig",
		Metadata:   types.ConfigMetadata{Name: "shutdown-test"},
		Settings:   types.GlobalSettings{NodeName: "test-node"},
	}
	if err := cfg.ApplyDefaults(); err != nil {
		t.Fatalf("ApplyDefaults: %v", err)
	}

	det, err := detector.NewProblemDetector(cfg, []types.Monitor{mon}, []types.Exporter{exp}, filepath.Join(t.TempDir(), "config.yaml"), nil, nil)
	if err != nil {
		t.Fatalf("NewProblemDetector: %v", err)
	}
	if err := det.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	if !shutdown(det, []ExporterLifecycle{exp}, 10*time.Second) {
		t.Fatal("shutdown timed out")
	}

	got := rec.snapshot()
	if len(got) != 2 || got[0] != "monitor" || got[1] != "exporter" {
		t.Fatalf("stop order = %v, want [monitor exporter]", got)
	}
	if exp.exportedAfterStop() {
		t.Fatal("ExportStatus was called after the exporter was stopped")
	}
}

type blockingStopper struct{}

func (blockingStopper) Stop() error {
	select {}
}

func TestShutdown_TimeoutReturnsFalse(t *testing.T) {
	start := time.Now()
	if shutdown(blockingStopper{}, nil, 50*time.Millisecond) {
		t.Fatal("shutdown reported success while the detector never stopped")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("shutdown took %v, want about 50ms", elapsed)
	}
}
