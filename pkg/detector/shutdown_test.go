package detector

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/supporttools/node-doctor/pkg/types"
)

type hungStopMonitor struct {
	stopBlock time.Duration
	release   chan struct{}
}

func (m *hungStopMonitor) Start() (<-chan *types.Status, error) {
	out := make(chan *types.Status)
	go func() {
		for {
			select {
			case <-m.release:
				return
			case out <- types.NewStatus("hung"):
			}
		}
	}()
	return out, nil
}

func (m *hungStopMonitor) Stop() {
	time.Sleep(m.stopBlock)
}

func TestStop_HungMonitorReturnsWithinTimeout(t *testing.T) {
	old := monitorStopTimeout
	monitorStopTimeout = 200 * time.Millisecond
	t.Cleanup(func() { monitorStopTimeout = old })

	mon := &hungStopMonitor{stopBlock: 2 * time.Second, release: make(chan struct{})}
	t.Cleanup(func() { close(mon.release) })

	cfg := &types.NodeDoctorConfig{
		APIVersion: "v1",
		Kind:       "NodeDoctorConfig",
		Metadata:   types.ConfigMetadata{Name: "hung-stop-test"},
		Settings:   types.GlobalSettings{NodeName: "test-node"},
	}
	if err := cfg.ApplyDefaults(); err != nil {
		t.Fatalf("ApplyDefaults: %v", err)
	}

	det, err := NewProblemDetector(cfg, []types.Monitor{mon}, []types.Exporter{NewMockExporter("e")}, filepath.Join(t.TempDir(), "config.yaml"), nil, nil)
	if err != nil {
		t.Fatalf("NewProblemDetector: %v", err)
	}
	if err := det.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	start := time.Now()
	if err := det.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("Stop took %v with a %v monitor stop timeout", elapsed, monitorStopTimeout)
	}
	if det.IsRunning() {
		t.Fatal("detector still reports running after Stop")
	}
}
