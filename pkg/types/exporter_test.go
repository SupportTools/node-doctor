package types

import (
	"errors"
	"testing"
)

// TestExporterReloadSummary_AddResult verifies AddResult method functionality.
func TestExporterReloadSummary_AddResult(t *testing.T) {
	tests := []struct {
		name                      string
		results                   []ExporterReloadResult
		expectedTotal             int
		expectedSuccessful        int
		expectedFailed            int
		expectedReloadableCount   int
	}{
		{
			name:                    "empty summary",
			results:                 []ExporterReloadResult{},
			expectedTotal:           0,
			expectedSuccessful:      0,
			expectedFailed:          0,
			expectedReloadableCount: 0,
		},
		{
			name: "single successful reload",
			results: []ExporterReloadResult{
				{ExporterType: "prometheus", Success: true, Message: "reloaded successfully"},
			},
			expectedTotal:           1,
			expectedSuccessful:      1,
			expectedFailed:          0,
			expectedReloadableCount: 0,
		},
		{
			name: "single failed reload",
			results: []ExporterReloadResult{
				{ExporterType: "kubernetes", Success: false, Error: errors.New("config invalid"), Message: "reload failed"},
			},
			expectedTotal:           1,
			expectedSuccessful:      0,
			expectedFailed:          1,
			expectedReloadableCount: 0,
		},
		{
			name: "mixed results",
			results: []ExporterReloadResult{
				{ExporterType: "prometheus", Success: true, Message: "reloaded successfully"},
				{ExporterType: "kubernetes", Success: false, Error: errors.New("config invalid")},
				{ExporterType: "http", Success: true, Message: "reloaded successfully"},
				{ExporterType: "logs", Success: false, Error: errors.New("not reloadable")},
			},
			expectedTotal:           4,
			expectedSuccessful:      2,
			expectedFailed:          2,
			expectedReloadableCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			summary := &ExporterReloadSummary{
				ReloadableCount: tt.expectedReloadableCount,
			}

			for _, result := range tt.results {
				summary.AddResult(result)
			}

			if summary.TotalExporters != tt.expectedTotal {
				t.Errorf("TotalExporters = %d, want %d", summary.TotalExporters, tt.expectedTotal)
			}
			if summary.SuccessfulReloads != tt.expectedSuccessful {
				t.Errorf("SuccessfulReloads = %d, want %d", summary.SuccessfulReloads, tt.expectedSuccessful)
			}
			if summary.FailedReloads != tt.expectedFailed {
				t.Errorf("FailedReloads = %d, want %d", summary.FailedReloads, tt.expectedFailed)
			}
			if len(summary.Results) != len(tt.results) {
				t.Errorf("Results count = %d, want %d", len(summary.Results), len(tt.results))
			}
		})
	}
}

// TestExporterReloadResult_Fields verifies ExporterReloadResult field assignments.
func TestExporterReloadResult_Fields(t *testing.T) {
	err := errors.New("test error")
	result := ExporterReloadResult{
		ExporterType: "prometheus",
		Success:      false,
		Error:        err,
		Message:      "config validation failed",
	}

	if result.ExporterType != "prometheus" {
		t.Errorf("ExporterType = %s, want prometheus", result.ExporterType)
	}
	if result.Success {
		t.Error("Success should be false")
	}
	if result.Error != err {
		t.Errorf("Error = %v, want %v", result.Error, err)
	}
	if result.Message != "config validation failed" {
		t.Errorf("Message = %s, want 'config validation failed'", result.Message)
	}
}

// TestExporterReloadSummary_InitialState verifies initial state of ExporterReloadSummary.
func TestExporterReloadSummary_InitialState(t *testing.T) {
	summary := ExporterReloadSummary{}

	if summary.TotalExporters != 0 {
		t.Errorf("TotalExporters = %d, want 0", summary.TotalExporters)
	}
	if summary.ReloadableCount != 0 {
		t.Errorf("ReloadableCount = %d, want 0", summary.ReloadableCount)
	}
	if summary.SuccessfulReloads != 0 {
		t.Errorf("SuccessfulReloads = %d, want 0", summary.SuccessfulReloads)
	}
	if summary.FailedReloads != 0 {
		t.Errorf("FailedReloads = %d, want 0", summary.FailedReloads)
	}
	if summary.Results != nil && len(summary.Results) != 0 {
		t.Errorf("Results should be nil or empty, got %d items", len(summary.Results))
	}
}

// TestExporterReloadSummary_ResultsPreserveOrder verifies results maintain insertion order.
func TestExporterReloadSummary_ResultsPreserveOrder(t *testing.T) {
	summary := &ExporterReloadSummary{}

	results := []ExporterReloadResult{
		{ExporterType: "first"},
		{ExporterType: "second"},
		{ExporterType: "third"},
	}

	for _, r := range results {
		summary.AddResult(r)
	}

	for i, r := range results {
		if summary.Results[i].ExporterType != r.ExporterType {
			t.Errorf("Results[%d].ExporterType = %s, want %s", i, summary.Results[i].ExporterType, r.ExporterType)
		}
	}
}
