package reload

import (
	"testing"

	"github.com/supporttools/node-doctor/pkg/types"
)

// TestComputeConfigDiff_NoChanges tests diff when configs are identical
func TestComputeConfigDiff_NoChanges(t *testing.T) {
	config := &types.NodeDoctorConfig{
		APIVersion: "node-doctor.io/v1alpha1",
		Kind:       "NodeDoctorConfig",
		Metadata: types.ConfigMetadata{
			Name: "test-config",
		},
		Settings: types.GlobalSettings{
			NodeName: "test-node",
		},
		Monitors: []types.MonitorConfig{
			{
				Name:    "monitor1",
				Type:    "system",
				Enabled: true,
				Config: map[string]interface{}{
					"interval": "30s",
				},
			},
		},
	}

	diff := ComputeConfigDiff(config, config)

	if diff.HasChanges() {
		t.Error("Expected no changes for identical configs")
	}

	if len(diff.MonitorsAdded) != 0 {
		t.Errorf("Expected 0 monitors added, got %d", len(diff.MonitorsAdded))
	}

	if len(diff.MonitorsRemoved) != 0 {
		t.Errorf("Expected 0 monitors removed, got %d", len(diff.MonitorsRemoved))
	}

	if len(diff.MonitorsModified) != 0 {
		t.Errorf("Expected 0 monitors modified, got %d", len(diff.MonitorsModified))
	}

	if diff.ExportersChanged {
		t.Error("Expected exporters unchanged")
	}

	if diff.RemediationChanged {
		t.Error("Expected remediation unchanged")
	}
}

// TestComputeConfigDiff_MonitorAdded tests adding a monitor
func TestComputeConfigDiff_MonitorAdded(t *testing.T) {
	oldConfig := &types.NodeDoctorConfig{
		Monitors: []types.MonitorConfig{
			{Name: "monitor1", Type: "system", Enabled: true},
		},
	}

	newConfig := &types.NodeDoctorConfig{
		Monitors: []types.MonitorConfig{
			{Name: "monitor1", Type: "system", Enabled: true},
			{Name: "monitor2", Type: "network", Enabled: true},
		},
	}

	diff := ComputeConfigDiff(oldConfig, newConfig)

	if !diff.HasChanges() {
		t.Error("Expected changes when monitor added")
	}

	if len(diff.MonitorsAdded) != 1 {
		t.Fatalf("Expected 1 monitor added, got %d", len(diff.MonitorsAdded))
	}

	if diff.MonitorsAdded[0].Name != "monitor2" {
		t.Errorf("Expected added monitor 'monitor2', got '%s'", diff.MonitorsAdded[0].Name)
	}

	if len(diff.MonitorsRemoved) != 0 {
		t.Errorf("Expected 0 monitors removed, got %d", len(diff.MonitorsRemoved))
	}

	if len(diff.MonitorsModified) != 0 {
		t.Errorf("Expected 0 monitors modified, got %d", len(diff.MonitorsModified))
	}
}

// TestComputeConfigDiff_MonitorRemoved tests removing a monitor
func TestComputeConfigDiff_MonitorRemoved(t *testing.T) {
	oldConfig := &types.NodeDoctorConfig{
		Monitors: []types.MonitorConfig{
			{Name: "monitor1", Type: "system", Enabled: true},
			{Name: "monitor2", Type: "network", Enabled: true},
		},
	}

	newConfig := &types.NodeDoctorConfig{
		Monitors: []types.MonitorConfig{
			{Name: "monitor1", Type: "system", Enabled: true},
		},
	}

	diff := ComputeConfigDiff(oldConfig, newConfig)

	if !diff.HasChanges() {
		t.Error("Expected changes when monitor removed")
	}

	if len(diff.MonitorsRemoved) != 1 {
		t.Fatalf("Expected 1 monitor removed, got %d", len(diff.MonitorsRemoved))
	}

	if diff.MonitorsRemoved[0].Name != "monitor2" {
		t.Errorf("Expected removed monitor 'monitor2', got '%s'", diff.MonitorsRemoved[0].Name)
	}

	if len(diff.MonitorsAdded) != 0 {
		t.Errorf("Expected 0 monitors added, got %d", len(diff.MonitorsAdded))
	}

	if len(diff.MonitorsModified) != 0 {
		t.Errorf("Expected 0 monitors modified, got %d", len(diff.MonitorsModified))
	}
}

// TestComputeConfigDiff_MonitorModified tests modifying a monitor
func TestComputeConfigDiff_MonitorModified(t *testing.T) {
	oldConfig := &types.NodeDoctorConfig{
		Monitors: []types.MonitorConfig{
			{
				Name:    "monitor1",
				Type:    "system",
				Enabled: true,
				Config: map[string]interface{}{
					"interval": "30s",
				},
			},
		},
	}

	newConfig := &types.NodeDoctorConfig{
		Monitors: []types.MonitorConfig{
			{
				Name:    "monitor1",
				Type:    "system",
				Enabled: true,
				Config: map[string]interface{}{
					"interval": "60s", // Changed
				},
			},
		},
	}

	diff := ComputeConfigDiff(oldConfig, newConfig)

	if !diff.HasChanges() {
		t.Error("Expected changes when monitor modified")
	}

	if len(diff.MonitorsModified) != 1 {
		t.Fatalf("Expected 1 monitor modified, got %d", len(diff.MonitorsModified))
	}

	if diff.MonitorsModified[0].Old.Name != "monitor1" {
		t.Errorf("Expected modified monitor old name 'monitor1', got '%s'", diff.MonitorsModified[0].Old.Name)
	}

	if diff.MonitorsModified[0].New.Name != "monitor1" {
		t.Errorf("Expected modified monitor new name 'monitor1', got '%s'", diff.MonitorsModified[0].New.Name)
	}

	oldInterval := diff.MonitorsModified[0].Old.Config["interval"]
	if oldInterval != "30s" {
		t.Errorf("Expected old interval '30s', got '%v'", oldInterval)
	}

	newInterval := diff.MonitorsModified[0].New.Config["interval"]
	if newInterval != "60s" {
		t.Errorf("Expected new interval '60s', got '%v'", newInterval)
	}

	if len(diff.MonitorsAdded) != 0 {
		t.Errorf("Expected 0 monitors added, got %d", len(diff.MonitorsAdded))
	}

	if len(diff.MonitorsRemoved) != 0 {
		t.Errorf("Expected 0 monitors removed, got %d", len(diff.MonitorsRemoved))
	}
}

// TestComputeConfigDiff_MonitorDisabled tests changing monitor enabled status
func TestComputeConfigDiff_MonitorDisabled(t *testing.T) {
	oldConfig := &types.NodeDoctorConfig{
		Monitors: []types.MonitorConfig{
			{Name: "monitor1", Type: "system", Enabled: true},
		},
	}

	newConfig := &types.NodeDoctorConfig{
		Monitors: []types.MonitorConfig{
			{Name: "monitor1", Type: "system", Enabled: false},
		},
	}

	diff := ComputeConfigDiff(oldConfig, newConfig)

	if !diff.HasChanges() {
		t.Error("Expected changes when monitor disabled")
	}

	if len(diff.MonitorsModified) != 1 {
		t.Fatalf("Expected 1 monitor modified, got %d", len(diff.MonitorsModified))
	}

	if diff.MonitorsModified[0].Old.Enabled != true {
		t.Error("Expected old monitor to be enabled")
	}

	if diff.MonitorsModified[0].New.Enabled != false {
		t.Error("Expected new monitor to be disabled")
	}
}

// TestComputeConfigDiff_MultipleChanges tests complex scenario
func TestComputeConfigDiff_MultipleChanges(t *testing.T) {
	oldConfig := &types.NodeDoctorConfig{
		Monitors: []types.MonitorConfig{
			{Name: "monitor1", Type: "system", Enabled: true},
			{Name: "monitor2", Type: "network", Enabled: true},
			{Name: "monitor3", Type: "disk", Enabled: true, Config: map[string]interface{}{"path": "/old"}},
		},
	}

	newConfig := &types.NodeDoctorConfig{
		Monitors: []types.MonitorConfig{
			{Name: "monitor1", Type: "system", Enabled: true},                                               // Unchanged
			{Name: "monitor3", Type: "disk", Enabled: true, Config: map[string]interface{}{"path": "/new"}}, // Modified
			{Name: "monitor4", Type: "memory", Enabled: true},                                               // Added
		},
		// monitor2 removed
	}

	diff := ComputeConfigDiff(oldConfig, newConfig)

	if !diff.HasChanges() {
		t.Error("Expected changes")
	}

	// Check added
	if len(diff.MonitorsAdded) != 1 {
		t.Errorf("Expected 1 monitor added, got %d", len(diff.MonitorsAdded))
	} else if diff.MonitorsAdded[0].Name != "monitor4" {
		t.Errorf("Expected 'monitor4' added, got '%s'", diff.MonitorsAdded[0].Name)
	}

	// Check removed
	if len(diff.MonitorsRemoved) != 1 {
		t.Errorf("Expected 1 monitor removed, got %d", len(diff.MonitorsRemoved))
	} else if diff.MonitorsRemoved[0].Name != "monitor2" {
		t.Errorf("Expected 'monitor2' removed, got '%s'", diff.MonitorsRemoved[0].Name)
	}

	// Check modified
	if len(diff.MonitorsModified) != 1 {
		t.Errorf("Expected 1 monitor modified, got %d", len(diff.MonitorsModified))
	} else if diff.MonitorsModified[0].Old.Name != "monitor3" {
		t.Errorf("Expected 'monitor3' modified, got '%s'", diff.MonitorsModified[0].Old.Name)
	}
}

// TestMonitorsEqual tests monitor equality checks
func TestMonitorsEqual(t *testing.T) {
	tests := []struct {
		name     string
		a        types.MonitorConfig
		b        types.MonitorConfig
		expected bool
	}{
		{
			name: "Identical monitors",
			a: types.MonitorConfig{
				Name:    "monitor1",
				Type:    "system",
				Enabled: true,
			},
			b: types.MonitorConfig{
				Name:    "monitor1",
				Type:    "system",
				Enabled: true,
			},
			expected: true,
		},
		{
			name: "Different enabled status",
			a: types.MonitorConfig{
				Name:    "monitor1",
				Type:    "system",
				Enabled: true,
			},
			b: types.MonitorConfig{
				Name:    "monitor1",
				Type:    "system",
				Enabled: false,
			},
			expected: false,
		},
		{
			name: "Different type",
			a: types.MonitorConfig{
				Name:    "monitor1",
				Type:    "system",
				Enabled: true,
			},
			b: types.MonitorConfig{
				Name:    "monitor1",
				Type:    "network",
				Enabled: true,
			},
			expected: false,
		},
		{
			name: "Different config",
			a: types.MonitorConfig{
				Name:    "monitor1",
				Type:    "system",
				Enabled: true,
				Config: map[string]interface{}{
					"interval": "30s",
				},
			},
			b: types.MonitorConfig{
				Name:    "monitor1",
				Type:    "system",
				Enabled: true,
				Config: map[string]interface{}{
					"interval": "60s",
				},
			},
			expected: false,
		},
		{
			name: "Config vs nil config",
			a: types.MonitorConfig{
				Name:    "monitor1",
				Type:    "system",
				Enabled: true,
				Config: map[string]interface{}{
					"interval": "30s",
				},
			},
			b: types.MonitorConfig{
				Name:    "monitor1",
				Type:    "system",
				Enabled: true,
			},
			expected: false,
		},
		{
			name: "Both nil configs",
			a: types.MonitorConfig{
				Name:    "monitor1",
				Type:    "system",
				Enabled: true,
			},
			b: types.MonitorConfig{
				Name:    "monitor1",
				Type:    "system",
				Enabled: true,
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := monitorsEqual(tt.a, tt.b)
			if result != tt.expected {
				t.Errorf("Expected monitorsEqual=%v, got %v", tt.expected, result)
			}
		})
	}
}

// TestExportersEqual tests exporter configuration equality
func TestExportersEqual(t *testing.T) {
	tests := []struct {
		name     string
		a        *types.ExporterConfigs
		b        *types.ExporterConfigs
		expected bool
	}{
		{
			name:     "Both nil",
			a:        nil,
			b:        nil,
			expected: true,
		},
		{
			name:     "One nil",
			a:        &types.ExporterConfigs{},
			b:        nil,
			expected: false,
		},
		{
			name: "Different Kubernetes config",
			a: &types.ExporterConfigs{
				Kubernetes: &types.KubernetesExporterConfig{
					Enabled: true,
				},
			},
			b: &types.ExporterConfigs{
				Kubernetes: &types.KubernetesExporterConfig{
					Enabled: false,
				},
			},
			expected: false,
		},
		{
			name: "Same Kubernetes config",
			a: &types.ExporterConfigs{
				Kubernetes: &types.KubernetesExporterConfig{
					Enabled: true,
				},
			},
			b: &types.ExporterConfigs{
				Kubernetes: &types.KubernetesExporterConfig{
					Enabled: true,
				},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := exportersEqual(tt.a, tt.b)
			if result != tt.expected {
				t.Errorf("Expected exportersEqual=%v, got %v", tt.expected, result)
			}
		})
	}
}

// TestRemediationEqual tests remediation configuration equality
func TestRemediationEqual(t *testing.T) {
	tests := []struct {
		name     string
		a        *types.RemediationConfig
		b        *types.RemediationConfig
		expected bool
	}{
		{
			name:     "Both nil",
			a:        nil,
			b:        nil,
			expected: true,
		},
		{
			name:     "One nil",
			a:        &types.RemediationConfig{},
			b:        nil,
			expected: false,
		},
		{
			name: "Different enabled status",
			a: &types.RemediationConfig{
				Enabled: true,
			},
			b: &types.RemediationConfig{
				Enabled: false,
			},
			expected: false,
		},
		{
			name: "Different dry run status",
			a: &types.RemediationConfig{
				Enabled: true,
				DryRun:  false,
			},
			b: &types.RemediationConfig{
				Enabled: true,
				DryRun:  true,
			},
			expected: false,
		},
		{
			name: "Same config",
			a: &types.RemediationConfig{
				Enabled: true,
				DryRun:  false,
			},
			b: &types.RemediationConfig{
				Enabled: true,
				DryRun:  false,
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := remediationEqual(tt.a, tt.b)
			if result != tt.expected {
				t.Errorf("Expected remediationEqual=%v, got %v", tt.expected, result)
			}
		})
	}
}

// TestConfigDiffHasChanges tests the HasChanges method
func TestConfigDiffHasChanges(t *testing.T) {
	tests := []struct {
		name     string
		diff     *ConfigDiff
		expected bool
	}{
		{
			name:     "Empty diff",
			diff:     &ConfigDiff{},
			expected: false,
		},
		{
			name: "Monitor added",
			diff: &ConfigDiff{
				MonitorsAdded: []types.MonitorConfig{{Name: "test"}},
			},
			expected: true,
		},
		{
			name: "Monitor removed",
			diff: &ConfigDiff{
				MonitorsRemoved: []types.MonitorConfig{{Name: "test"}},
			},
			expected: true,
		},
		{
			name: "Monitor modified",
			diff: &ConfigDiff{
				MonitorsModified: []MonitorChange{{
					Old: types.MonitorConfig{Name: "test"},
					New: types.MonitorConfig{Name: "test"},
				}},
			},
			expected: true,
		},
		{
			name: "Exporters changed",
			diff: &ConfigDiff{
				ExportersChanged: true,
			},
			expected: true,
		},
		{
			name: "Remediation changed",
			diff: &ConfigDiff{
				RemediationChanged: true,
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.diff.HasChanges()
			if result != tt.expected {
				t.Errorf("Expected HasChanges()=%v, got %v", tt.expected, result)
			}
		})
	}
}
