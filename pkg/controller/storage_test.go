package controller

import (
	"context"
	"testing"
	"time"
)

// newTestStorage creates a new SQLite storage instance for testing
// using an in-memory database
func newTestStorage(t *testing.T) *SQLiteStorage {
	t.Helper()

	config := &StorageConfig{
		Path:      ":memory:", // In-memory SQLite
		Retention: 24 * time.Hour,
	}

	storage, err := NewSQLiteStorage(config)
	if err != nil {
		t.Fatalf("NewSQLiteStorage() error = %v", err)
	}

	ctx := context.Background()
	if err := storage.Initialize(ctx); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	t.Cleanup(func() {
		storage.Close()
	})

	return storage
}

func TestNewSQLiteStorage(t *testing.T) {
	t.Run("creates storage with valid config", func(t *testing.T) {
		config := &StorageConfig{
			Path:      ":memory:",
			Retention: 24 * time.Hour,
		}

		storage, err := NewSQLiteStorage(config)
		if err != nil {
			t.Fatalf("NewSQLiteStorage() error = %v", err)
		}
		if storage == nil {
			t.Fatal("NewSQLiteStorage() returned nil")
		}
	})

	t.Run("returns error with nil config", func(t *testing.T) {
		_, err := NewSQLiteStorage(nil)
		if err == nil {
			t.Error("expected error for nil config")
		}
	})
}

func TestSQLiteStorage_Initialize(t *testing.T) {
	config := &StorageConfig{
		Path:      ":memory:",
		Retention: 24 * time.Hour,
	}

	storage, _ := NewSQLiteStorage(config)

	ctx := context.Background()
	if err := storage.Initialize(ctx); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	// Verify tables were created by running a simple query
	var count int
	err := storage.db.QueryRow("SELECT COUNT(*) FROM node_reports").Scan(&count)
	if err != nil {
		t.Errorf("node_reports table should exist: %v", err)
	}

	err = storage.db.QueryRow("SELECT COUNT(*) FROM leases").Scan(&count)
	if err != nil {
		t.Errorf("leases table should exist: %v", err)
	}

	err = storage.db.QueryRow("SELECT COUNT(*) FROM correlations").Scan(&count)
	if err != nil {
		t.Errorf("correlations table should exist: %v", err)
	}

	err = storage.db.QueryRow("SELECT COUNT(*) FROM events").Scan(&count)
	if err != nil {
		t.Errorf("events table should exist: %v", err)
	}

	storage.Close()
}

func TestSQLiteStorage_NodeReports(t *testing.T) {
	storage := newTestStorage(t)
	ctx := context.Background()

	t.Run("save and retrieve report", func(t *testing.T) {
		report := &NodeReport{
			NodeName:      "test-node-1",
			NodeUID:       "uid-123",
			ReportID:      "report-1",
			Timestamp:     time.Now(),
			OverallHealth: HealthStatusHealthy,
			ActiveProblems: []ProblemSummary{
				{Type: "dns", Severity: "warning", Message: "DNS slow"},
			},
		}

		if err := storage.SaveNodeReport(ctx, report); err != nil {
			t.Fatalf("SaveNodeReport() error = %v", err)
		}

		retrieved, err := storage.GetLatestNodeReport(ctx, "test-node-1")
		if err != nil {
			t.Fatalf("GetLatestNodeReport() error = %v", err)
		}

		if retrieved == nil {
			t.Fatal("GetLatestNodeReport() returned nil")
		}
		if retrieved.NodeName != "test-node-1" {
			t.Errorf("expected NodeName 'test-node-1', got %q", retrieved.NodeName)
		}
		if retrieved.OverallHealth != HealthStatusHealthy {
			t.Errorf("expected HealthStatusHealthy, got %q", retrieved.OverallHealth)
		}
		if len(retrieved.ActiveProblems) != 1 {
			t.Errorf("expected 1 active problem, got %d", len(retrieved.ActiveProblems))
		}
	})

	t.Run("get non-existent node", func(t *testing.T) {
		retrieved, err := storage.GetLatestNodeReport(ctx, "nonexistent")
		if err != nil {
			t.Fatalf("GetLatestNodeReport() error = %v", err)
		}
		if retrieved != nil {
			t.Error("expected nil for non-existent node")
		}
	})

	t.Run("get multiple reports for node", func(t *testing.T) {
		// Save multiple reports
		for i := 0; i < 5; i++ {
			report := &NodeReport{
				NodeName:      "multi-node",
				ReportID:      "report-" + string(rune('a'+i)),
				Timestamp:     time.Now().Add(time.Duration(i) * time.Minute),
				OverallHealth: HealthStatusHealthy,
			}
			storage.SaveNodeReport(ctx, report)
		}

		reports, err := storage.GetNodeReports(ctx, "multi-node", 3)
		if err != nil {
			t.Fatalf("GetNodeReports() error = %v", err)
		}

		if len(reports) != 3 {
			t.Errorf("expected 3 reports, got %d", len(reports))
		}
	})

	t.Run("get all latest reports", func(t *testing.T) {
		// Add another node
		storage.SaveNodeReport(ctx, &NodeReport{
			NodeName:      "node-2",
			Timestamp:     time.Now(),
			OverallHealth: HealthStatusDegraded,
		})

		reports, err := storage.GetAllLatestReports(ctx)
		if err != nil {
			t.Fatalf("GetAllLatestReports() error = %v", err)
		}

		if len(reports) < 2 {
			t.Errorf("expected at least 2 nodes, got %d", len(reports))
		}
	})

	t.Run("delete old reports", func(t *testing.T) {
		// Save an old report
		oldReport := &NodeReport{
			NodeName:      "old-node",
			Timestamp:     time.Now().Add(-48 * time.Hour),
			OverallHealth: HealthStatusHealthy,
		}
		storage.SaveNodeReport(ctx, oldReport)

		// Delete reports older than 24 hours
		cutoff := time.Now().Add(-24 * time.Hour)
		deleted, err := storage.DeleteOldReports(ctx, cutoff)
		if err != nil {
			t.Fatalf("DeleteOldReports() error = %v", err)
		}

		if deleted < 1 {
			t.Errorf("expected at least 1 deleted report, got %d", deleted)
		}

		// Verify old report is gone
		report, _ := storage.GetLatestNodeReport(ctx, "old-node")
		if report != nil {
			t.Error("old report should have been deleted")
		}
	})
}

func TestSQLiteStorage_Leases(t *testing.T) {
	storage := newTestStorage(t)
	ctx := context.Background()

	t.Run("save and retrieve lease", func(t *testing.T) {
		lease := &Lease{
			ID:              "lease-123",
			NodeName:        "node-1",
			RemediationType: "restart-kubelet",
			Status:          "active",
			Reason:          "kubelet unresponsive",
			GrantedAt:       time.Now(),
			ExpiresAt:       time.Now().Add(5 * time.Minute),
		}

		if err := storage.SaveLease(ctx, lease); err != nil {
			t.Fatalf("SaveLease() error = %v", err)
		}

		retrieved, err := storage.GetLease(ctx, "lease-123")
		if err != nil {
			t.Fatalf("GetLease() error = %v", err)
		}

		if retrieved == nil {
			t.Fatal("GetLease() returned nil")
		}
		if retrieved.ID != "lease-123" {
			t.Errorf("expected ID 'lease-123', got %q", retrieved.ID)
		}
		if retrieved.NodeName != "node-1" {
			t.Errorf("expected NodeName 'node-1', got %q", retrieved.NodeName)
		}
	})

	t.Run("get non-existent lease", func(t *testing.T) {
		retrieved, err := storage.GetLease(ctx, "nonexistent")
		if err != nil {
			t.Fatalf("GetLease() error = %v", err)
		}
		if retrieved != nil {
			t.Error("expected nil for non-existent lease")
		}
	})

	t.Run("get active leases", func(t *testing.T) {
		// Add another active lease
		storage.SaveLease(ctx, &Lease{
			ID:              "lease-456",
			NodeName:        "node-2",
			RemediationType: "flush-dns",
			Status:          "active",
			GrantedAt:       time.Now(),
			ExpiresAt:       time.Now().Add(5 * time.Minute),
		})

		leases, err := storage.GetActiveLeases(ctx)
		if err != nil {
			t.Fatalf("GetActiveLeases() error = %v", err)
		}

		if len(leases) < 2 {
			t.Errorf("expected at least 2 active leases, got %d", len(leases))
		}
	})

	t.Run("get node lease", func(t *testing.T) {
		lease, err := storage.GetNodeLease(ctx, "node-1")
		if err != nil {
			t.Fatalf("GetNodeLease() error = %v", err)
		}

		if lease == nil {
			t.Fatal("GetNodeLease() returned nil")
		}
		if lease.NodeName != "node-1" {
			t.Errorf("expected NodeName 'node-1', got %q", lease.NodeName)
		}
	})

	t.Run("update lease status", func(t *testing.T) {
		if err := storage.UpdateLeaseStatus(ctx, "lease-123", "completed"); err != nil {
			t.Fatalf("UpdateLeaseStatus() error = %v", err)
		}

		lease, _ := storage.GetLease(ctx, "lease-123")
		if lease.Status != "completed" {
			t.Errorf("expected status 'completed', got %q", lease.Status)
		}
	})

	t.Run("expire leases", func(t *testing.T) {
		// Add an expired lease
		storage.SaveLease(ctx, &Lease{
			ID:              "lease-expired",
			NodeName:        "node-3",
			RemediationType: "test",
			Status:          "active",
			GrantedAt:       time.Now().Add(-10 * time.Minute),
			ExpiresAt:       time.Now().Add(-5 * time.Minute), // Already expired
		})

		expired, err := storage.ExpireLeases(ctx)
		if err != nil {
			t.Fatalf("ExpireLeases() error = %v", err)
		}

		if expired < 1 {
			t.Errorf("expected at least 1 expired lease, got %d", expired)
		}

		// Verify lease is expired
		lease, _ := storage.GetLease(ctx, "lease-expired")
		if lease.Status != "expired" {
			t.Errorf("expected status 'expired', got %q", lease.Status)
		}
	})
}

func TestSQLiteStorage_GetLastCompletedLease(t *testing.T) {
	storage := newTestStorage(t)
	ctx := context.Background()

	t.Run("returns nil when no completed leases exist", func(t *testing.T) {
		lease, err := storage.GetLastCompletedLease(ctx, "nonexistent-node")
		if err != nil {
			t.Fatalf("GetLastCompletedLease() error = %v", err)
		}
		if lease != nil {
			t.Error("expected nil for node with no leases")
		}
	})

	t.Run("ignores active leases", func(t *testing.T) {
		// Save an active lease
		storage.SaveLease(ctx, &Lease{
			ID:              "active-lease-1",
			NodeName:        "cooldown-node-1",
			RemediationType: "restart-kubelet",
			Status:          "active",
			GrantedAt:       time.Now(),
			ExpiresAt:       time.Now().Add(5 * time.Minute),
		})

		lease, err := storage.GetLastCompletedLease(ctx, "cooldown-node-1")
		if err != nil {
			t.Fatalf("GetLastCompletedLease() error = %v", err)
		}
		if lease != nil {
			t.Error("expected nil for node with only active leases")
		}
	})

	t.Run("returns completed lease", func(t *testing.T) {
		// Save and complete a lease
		storage.SaveLease(ctx, &Lease{
			ID:              "completed-lease-1",
			NodeName:        "cooldown-node-2",
			RemediationType: "restart-kubelet",
			Status:          "active",
			GrantedAt:       time.Now().Add(-10 * time.Minute),
			ExpiresAt:       time.Now().Add(-5 * time.Minute),
		})
		storage.UpdateLeaseStatus(ctx, "completed-lease-1", "completed")

		lease, err := storage.GetLastCompletedLease(ctx, "cooldown-node-2")
		if err != nil {
			t.Fatalf("GetLastCompletedLease() error = %v", err)
		}
		if lease == nil {
			t.Fatal("expected completed lease, got nil")
		}
		if lease.ID != "completed-lease-1" {
			t.Errorf("expected lease ID 'completed-lease-1', got %q", lease.ID)
		}
		if lease.Status != "completed" {
			t.Errorf("expected status 'completed', got %q", lease.Status)
		}
		if lease.CompletedAt.IsZero() {
			t.Error("expected CompletedAt to be set")
		}
	})

	t.Run("returns expired lease", func(t *testing.T) {
		// Save an expired lease
		storage.SaveLease(ctx, &Lease{
			ID:              "expired-lease-1",
			NodeName:        "cooldown-node-3",
			RemediationType: "flush-dns",
			Status:          "active",
			GrantedAt:       time.Now().Add(-10 * time.Minute),
			ExpiresAt:       time.Now().Add(-5 * time.Minute),
		})
		storage.UpdateLeaseStatus(ctx, "expired-lease-1", "expired")

		lease, err := storage.GetLastCompletedLease(ctx, "cooldown-node-3")
		if err != nil {
			t.Fatalf("GetLastCompletedLease() error = %v", err)
		}
		if lease == nil {
			t.Fatal("expected expired lease, got nil")
		}
		if lease.Status != "expired" {
			t.Errorf("expected status 'expired', got %q", lease.Status)
		}
	})

	t.Run("returns most recent completed lease", func(t *testing.T) {
		// Save multiple completed leases with different completed_at times
		storage.SaveLease(ctx, &Lease{
			ID:              "old-completed-lease",
			NodeName:        "cooldown-node-4",
			RemediationType: "restart-kubelet",
			Status:          "active",
			GrantedAt:       time.Now().Add(-30 * time.Minute),
			ExpiresAt:       time.Now().Add(-25 * time.Minute),
		})
		storage.UpdateLeaseStatus(ctx, "old-completed-lease", "completed")

		// Wait a tiny bit to ensure different completed_at timestamps
		time.Sleep(10 * time.Millisecond)

		storage.SaveLease(ctx, &Lease{
			ID:              "recent-completed-lease",
			NodeName:        "cooldown-node-4",
			RemediationType: "flush-dns",
			Status:          "active",
			GrantedAt:       time.Now().Add(-10 * time.Minute),
			ExpiresAt:       time.Now().Add(-5 * time.Minute),
		})
		storage.UpdateLeaseStatus(ctx, "recent-completed-lease", "completed")

		lease, err := storage.GetLastCompletedLease(ctx, "cooldown-node-4")
		if err != nil {
			t.Fatalf("GetLastCompletedLease() error = %v", err)
		}
		if lease == nil {
			t.Fatal("expected completed lease, got nil")
		}
		if lease.ID != "recent-completed-lease" {
			t.Errorf("expected most recent lease 'recent-completed-lease', got %q", lease.ID)
		}
		if lease.RemediationType != "flush-dns" {
			t.Errorf("expected remediation type 'flush-dns', got %q", lease.RemediationType)
		}
	})
}

func TestSQLiteStorage_Correlations(t *testing.T) {
	storage := newTestStorage(t)
	ctx := context.Background()

	t.Run("save and retrieve correlation", func(t *testing.T) {
		correlation := &Correlation{
			ID:            "corr-123",
			Type:          "infrastructure",
			Severity:      "warning",
			Status:        "active",
			Message:       "Multiple nodes experiencing DNS issues",
			Confidence:    0.85,
			AffectedNodes: []string{"node-1", "node-2", "node-3"},
			ProblemTypes:  []string{"dns", "network"},
			DetectedAt:    time.Now(),
			UpdatedAt:     time.Now(),
			Metadata: map[string]interface{}{
				"affectedPercentage": 0.3,
			},
		}

		if err := storage.SaveCorrelation(ctx, correlation); err != nil {
			t.Fatalf("SaveCorrelation() error = %v", err)
		}

		retrieved, err := storage.GetCorrelation(ctx, "corr-123")
		if err != nil {
			t.Fatalf("GetCorrelation() error = %v", err)
		}

		if retrieved == nil {
			t.Fatal("GetCorrelation() returned nil")
		}
		if retrieved.ID != "corr-123" {
			t.Errorf("expected ID 'corr-123', got %q", retrieved.ID)
		}
		if retrieved.Type != "infrastructure" {
			t.Errorf("expected Type 'infrastructure', got %q", retrieved.Type)
		}
		if len(retrieved.AffectedNodes) != 3 {
			t.Errorf("expected 3 affected nodes, got %d", len(retrieved.AffectedNodes))
		}
		if retrieved.Confidence != 0.85 {
			t.Errorf("expected Confidence 0.85, got %f", retrieved.Confidence)
		}
	})

	t.Run("get non-existent correlation", func(t *testing.T) {
		retrieved, err := storage.GetCorrelation(ctx, "nonexistent")
		if err != nil {
			t.Fatalf("GetCorrelation() error = %v", err)
		}
		if retrieved != nil {
			t.Error("expected nil for non-existent correlation")
		}
	})

	t.Run("get active correlations", func(t *testing.T) {
		correlations, err := storage.GetActiveCorrelations(ctx)
		if err != nil {
			t.Fatalf("GetActiveCorrelations() error = %v", err)
		}

		if len(correlations) < 1 {
			t.Errorf("expected at least 1 active correlation, got %d", len(correlations))
		}
	})

	t.Run("update correlation", func(t *testing.T) {
		correlation := &Correlation{
			ID:            "corr-123",
			Type:          "infrastructure",
			Severity:      "critical", // Updated severity
			Status:        "active",
			Message:       "Multiple nodes experiencing DNS issues",
			Confidence:    0.95,                                             // Updated confidence
			AffectedNodes: []string{"node-1", "node-2", "node-3", "node-4"}, // Added node
			DetectedAt:    time.Now(),
			UpdatedAt:     time.Now(),
		}

		if err := storage.UpdateCorrelation(ctx, correlation); err != nil {
			t.Fatalf("UpdateCorrelation() error = %v", err)
		}

		retrieved, _ := storage.GetCorrelation(ctx, "corr-123")
		if retrieved.Severity != "critical" {
			t.Errorf("expected Severity 'critical', got %q", retrieved.Severity)
		}
		if len(retrieved.AffectedNodes) != 4 {
			t.Errorf("expected 4 affected nodes, got %d", len(retrieved.AffectedNodes))
		}
	})
}

func TestSQLiteStorage_Statistics(t *testing.T) {
	storage := newTestStorage(t)
	ctx := context.Background()

	// Add test data
	for i := 0; i < 3; i++ {
		storage.SaveNodeReport(ctx, &NodeReport{
			NodeName:      "stats-node-" + string(rune('a'+i)),
			Timestamp:     time.Now(),
			OverallHealth: HealthStatusHealthy,
		})
	}

	storage.SaveLease(ctx, &Lease{
		ID:        "stats-lease-1",
		NodeName:  "stats-node-a",
		Status:    "active",
		GrantedAt: time.Now(),
		ExpiresAt: time.Now().Add(5 * time.Minute),
	})

	t.Run("get node count", func(t *testing.T) {
		count, err := storage.GetNodeCount(ctx)
		if err != nil {
			t.Fatalf("GetNodeCount() error = %v", err)
		}
		if count < 3 {
			t.Errorf("expected at least 3 nodes, got %d", count)
		}
	})

	t.Run("get report count", func(t *testing.T) {
		count, err := storage.GetReportCount(ctx)
		if err != nil {
			t.Fatalf("GetReportCount() error = %v", err)
		}
		if count < 3 {
			t.Errorf("expected at least 3 reports, got %d", count)
		}
	})

	t.Run("get lease stats", func(t *testing.T) {
		active, total, err := storage.GetLeaseStats(ctx)
		if err != nil {
			t.Fatalf("GetLeaseStats() error = %v", err)
		}
		if total < 1 {
			t.Errorf("expected at least 1 total lease, got %d", total)
		}
		if active < 1 {
			t.Errorf("expected at least 1 active lease, got %d", active)
		}
	})
}

func TestSQLiteStorage_RunCleanup(t *testing.T) {
	config := &StorageConfig{
		Path:      ":memory:",
		Retention: time.Hour, // Short retention for testing
	}

	storage, _ := NewSQLiteStorage(config)
	storage.Initialize(context.Background())
	defer storage.Close()

	ctx := context.Background()

	// Add old report
	storage.SaveNodeReport(ctx, &NodeReport{
		NodeName:      "cleanup-node",
		Timestamp:     time.Now().Add(-2 * time.Hour), // Older than retention
		OverallHealth: HealthStatusHealthy,
	})

	// Add old expired lease
	storage.SaveLease(ctx, &Lease{
		ID:        "cleanup-lease",
		NodeName:  "cleanup-node",
		Status:    "active",
		GrantedAt: time.Now().Add(-2 * time.Hour),
		ExpiresAt: time.Now().Add(-1 * time.Hour), // Expired
	})

	// Run cleanup
	if err := storage.RunCleanup(ctx); err != nil {
		t.Fatalf("RunCleanup() error = %v", err)
	}

	// Verify old report is gone
	report, _ := storage.GetLatestNodeReport(ctx, "cleanup-node")
	if report != nil {
		t.Error("old report should have been cleaned up")
	}

	// Verify lease is expired
	lease, _ := storage.GetLease(ctx, "cleanup-lease")
	if lease != nil && lease.Status != "expired" {
		t.Errorf("expected lease status 'expired', got %q", lease.Status)
	}
}
