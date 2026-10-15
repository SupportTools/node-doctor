package types

import (
	"strings"
	"testing"
	"time"
)

func validPolicy() *MonitorRemediationConfig {
	return &MonitorRemediationConfig{
		Enabled:                true,
		Strategy:               "systemd-restart",
		Service:                "rke2-agent",
		RequireServiceEnabled:  true,
		TriggerConditions:      []string{"KubeletUnhealthy"},
		MaxAttemptsPerIncident: 1,
		CooldownString:         "30m",
		Preconditions: []RemediationPrecondition{{
			Type: "ReadonlyFilesystem", Status: "False", Monitor: "disk-health", MaxAgeString: "3m",
		}},
	}
}

func TestRemediationPolicy_ParsesAndValidates(t *testing.T) {
	r := validPolicy()
	if err := r.ApplyDefaults(); err != nil {
		t.Fatalf("ApplyDefaults: %v", err)
	}
	if r.Cooldown != 30*time.Minute {
		t.Errorf("Cooldown = %v, want 30m", r.Cooldown)
	}
	if r.Preconditions[0].MaxAge != 3*time.Minute {
		t.Errorf("MaxAge = %v, want 3m", r.Preconditions[0].MaxAge)
	}
	if err := r.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestRemediationPolicy_Rejects(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*MonitorRemediationConfig)
		wantErr string
	}{
		{"bad cooldown", func(r *MonitorRemediationConfig) { r.CooldownString = "soon" }, "invalid cooldown"},
		{"bad maxAge", func(r *MonitorRemediationConfig) { r.Preconditions[0].MaxAgeString = "x" }, "invalid maxAge"},
		{"missing maxAge", func(r *MonitorRemediationConfig) { r.Preconditions[0].MaxAgeString = "" }, "maxAge is required"},
		{"bad status", func(r *MonitorRemediationConfig) { r.Preconditions[0].Status = "false" }, "status must be"},
		{"missing type", func(r *MonitorRemediationConfig) { r.Preconditions[0].Type = "" }, "type is required"},
		{"negative max", func(r *MonitorRemediationConfig) { r.MaxAttemptsPerIncident = -1 }, "maxAttemptsPerIncident"},
		{"empty trigger", func(r *MonitorRemediationConfig) { r.TriggerConditions = []string{" "} }, "triggerConditions[0]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := validPolicy()
			tt.mutate(r)
			err := r.ApplyDefaults()
			if err == nil {
				err = r.Validate()
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}
