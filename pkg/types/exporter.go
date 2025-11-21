package types

// ReloadableExporter extends the basic Exporter interface with reload capability.
// Exporters that implement this interface can update their configuration without
// requiring a full restart, enabling hot reload of exporter settings.
type ReloadableExporter interface {
	Exporter

	// Reload updates the exporter configuration without restarting the exporter.
	// The config parameter should be the exporter-specific configuration struct.
	// Returns an error if the reload fails or if the configuration is invalid.
	Reload(config interface{}) error

	// IsReloadable returns true if this exporter supports configuration reload.
	// This is primarily used for runtime checks and debugging.
	IsReloadable() bool
}

// ExporterReloadResult represents the result of an exporter reload operation
type ExporterReloadResult struct {
	ExporterType string // Type of exporter (e.g., "kubernetes", "http", "prometheus")
	Success      bool   // Whether the reload was successful
	Error        error  // Error details if reload failed
	Message      string // Additional information about the reload
}

// ExporterReloadSummary provides a summary of all exporter reload operations
type ExporterReloadSummary struct {
	TotalExporters    int                     // Total number of exporters
	ReloadableCount   int                     // Number of exporters that support reload
	SuccessfulReloads int                     // Number of successful reloads
	FailedReloads     int                     // Number of failed reloads
	Results           []ExporterReloadResult  // Detailed results for each exporter
}

// AddResult adds a reload result to the summary
func (s *ExporterReloadSummary) AddResult(result ExporterReloadResult) {
	s.Results = append(s.Results, result)
	s.TotalExporters++

	if result.Success {
		s.SuccessfulReloads++
	} else {
		s.FailedReloads++
	}
}