package types

import (
	"errors"
	"strings"
	"testing"
	"time"
)

type recordingRegistry struct {
	mockMonitorRegistry
	validated []string
	failFor   string
}

func (r *recordingRegistry) ValidateConfig(config MonitorConfig) error {
	r.validated = append(r.validated, config.Name)
	if config.Name == r.failFor {
		return errors.New("bad threshold")
	}
	return nil
}

func registryValidationConfig() NodeDoctorConfig {
	return NodeDoctorConfig{
		APIVersion: "v1",
		Kind:       "NodeDoctorConfig",
		Metadata:   ConfigMetadata{Name: "t"},
		Settings: GlobalSettings{
			NodeName:          "n",
			LogLevel:          "info",
			LogFormat:         "json",
			LogOutput:         "stdout",
			UpdateInterval:    10 * time.Second,
			ResyncInterval:    60 * time.Second,
			HeartbeatInterval: 5 * time.Second,
			QPS:               50,
			Burst:             100,
		},
		Monitors: []MonitorConfig{
			{Name: "cpu", Type: "system-cpu", Enabled: true, Interval: 30 * time.Second, Timeout: 10 * time.Second},
			{Name: "mem", Type: "system-memory", Enabled: false, Interval: 30 * time.Second, Timeout: 10 * time.Second},
		},
		Remediation: RemediationConfig{
			CooldownPeriod:           5 * time.Minute,
			MaxAttemptsGlobal:        3,
			MaxRemediationsPerHour:   10,
			MaxRemediationsPerMinute: 2,
			HistorySize:              100,
		},
	}
}

func TestValidateWithRegistryRunsTypeValidatorsForEnabledMonitors(t *testing.T) {
	reg := &recordingRegistry{mockMonitorRegistry: mockMonitorRegistry{
		registeredTypes: map[string]bool{"system-cpu": true, "system-memory": true},
	}}
	cfg := registryValidationConfig()
	if err := cfg.ValidateWithRegistry(reg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(reg.validated) != 1 || reg.validated[0] != "cpu" {
		t.Fatalf("expected only enabled monitors validated, got %v", reg.validated)
	}
}

func TestValidateWithRegistryPropagatesTypeValidatorError(t *testing.T) {
	reg := &recordingRegistry{
		mockMonitorRegistry: mockMonitorRegistry{registeredTypes: map[string]bool{"system-cpu": true, "system-memory": true}},
		failFor:             "cpu",
	}
	cfg := registryValidationConfig()
	err := cfg.ValidateWithRegistry(reg)
	if err == nil || !strings.Contains(err.Error(), `monitor "cpu": bad threshold`) {
		t.Fatalf("expected wrapped validator error, got %v", err)
	}
}
