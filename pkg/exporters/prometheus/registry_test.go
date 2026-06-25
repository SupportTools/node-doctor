package prometheus

import (
	"runtime"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

// TestNewRegistry_IncludesGoAndProcessCollectors pins the contract for task
// #17210: the registry the exporter serves wires the standard Go-runtime and
// process collectors so go_* and process_* metrics are exposed alongside the
// node-doctor metrics. NewRegistry is the registry actually used by the
// exporter (see exporter.go), so this guards against a regression that would
// silently drop runtime/process self-observability.
func TestNewRegistry_IncludesGoAndProcessCollectors(t *testing.T) {
	reg := NewRegistry(prometheus.Labels{"node": "test-node"})

	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("registry.Gather() error: %v", err)
	}

	families := make(map[string]bool, len(mfs))
	var goCount, processCount int
	for _, mf := range mfs {
		name := mf.GetName()
		families[name] = true
		if strings.HasPrefix(name, "go_") {
			goCount++
		}
		if strings.HasPrefix(name, "process_") {
			processCount++
		}
	}

	// The Go collector is available on every platform; go_goroutines is a
	// stable, always-present series.
	if !families["go_goroutines"] {
		t.Errorf("expected go_goroutines from the Go collector; got %d go_* families", goCount)
	}

	// The process collector only emits metrics on platforms it supports
	// (Linux in CI/production). Guard so the test stays green elsewhere.
	if runtime.GOOS == "linux" {
		if processCount == 0 {
			t.Errorf("expected process_* metrics from the process collector on linux, got none")
		}
		if !families["process_start_time_seconds"] {
			t.Errorf("expected process_start_time_seconds from the process collector on linux")
		}
	}
}
