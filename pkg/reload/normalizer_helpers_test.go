package reload

import (
	"errors"
	"fmt"
	"testing"

	"github.com/supporttools/node-doctor/pkg/monitors"
	"github.com/supporttools/node-doctor/pkg/types"
	"github.com/supporttools/node-doctor/pkg/util"
)

var errBoom = errors.New("boom")

func sprint(v interface{}) string { return fmt.Sprint(v) }

// normalizeForTest mirrors the normalizer main.go installs: registry default
// monitors, then ApplyDefaults. It deliberately has the same shape as the
// production closure so these tests exercise the real symmetry requirement.
func normalizeForTest(c *types.NodeDoctorConfig, applyDefaultMonitors bool) error {
	if applyDefaultMonitors {
		monitors.ApplyDefaultMonitors(c)
	}
	return c.ApplyDefaults()
}

// loadAndNormalize performs the startup sequence: load the file, then normalize.
func loadAndNormalize(t *testing.T, path string, applyDefaultMonitors bool) *types.NodeDoctorConfig {
	t.Helper()
	cfg, err := util.LoadConfig(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if err := normalizeForTest(cfg, applyDefaultMonitors); err != nil {
		t.Fatalf("normalize config: %v", err)
	}
	return cfg
}
