package network

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// newTestStore creates a SQLiteMetricsStore backed by a temp-dir database file.
// The caller is responsible for calling store.Close() and cleaning up the dir.
func newTestStore(t *testing.T) (*SQLiteMetricsStore, string) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "dns-metrics-test.db")
	cfg := &HistoricalMetricsConfig{
		Enabled:       true,
		StoragePath:   dbPath,
		RetentionDays: 7,
	}
	store, err := NewSQLiteMetricsStore(cfg)
	if err != nil {
		t.Fatalf("NewSQLiteMetricsStore: %v", err)
	}
	if err := store.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	return store, dir
}

func TestNewSQLiteMetricsStore_NilConfig(t *testing.T) {
	_, err := NewSQLiteMetricsStore(nil)
	if err == nil {
		t.Fatal("expected error for nil config, got nil")
	}
}

func TestInitialize_IdempotentError(t *testing.T) {
	store, _ := newTestStore(t)
	defer store.Close()

	err := store.Initialize(context.Background())
	if err == nil {
		t.Fatal("expected error on second Initialize call, got nil")
	}
}

func TestRecord_and_QueryRange(t *testing.T) {
	store, _ := newTestStore(t)
	defer store.Close()

	ctx := context.Background()
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	ns := "10.96.0.10"

	// Record 5 samples: 3 success, 2 failure.
	samples := []DNSMetricSample{
		{Timestamp: base.Add(0 * time.Second), Nameserver: ns, Domain: "k8s.local", Success: true, LatencyMs: 5},
		{Timestamp: base.Add(1 * time.Second), Nameserver: ns, Domain: "k8s.local", Success: false, LatencyMs: 0, ErrorType: "TIMEOUT"},
		{Timestamp: base.Add(2 * time.Second), Nameserver: ns, Domain: "k8s.local", Success: true, LatencyMs: 8},
		{Timestamp: base.Add(3 * time.Second), Nameserver: ns, Domain: "k8s.local", Success: true, LatencyMs: 6},
		{Timestamp: base.Add(4 * time.Second), Nameserver: ns, Domain: "k8s.local", Success: false, LatencyMs: 0, ErrorType: "SERVFAIL"},
	}
	for i, s := range samples {
		if err := store.Record(ctx, s); err != nil {
			t.Fatalf("Record[%d]: %v", i, err)
		}
	}

	got, err := store.QueryRange(ctx, ns, base.Add(-1*time.Second), base.Add(10*time.Second))
	if err != nil {
		t.Fatalf("QueryRange: %v", err)
	}
	if len(got) != len(samples) {
		t.Fatalf("QueryRange returned %d rows, want %d", len(got), len(samples))
	}

	// Verify ordering (oldest first) and field round-trip.
	for i, s := range got {
		want := samples[i]
		if s.Nameserver != want.Nameserver {
			t.Errorf("row %d: Nameserver=%q, want %q", i, s.Nameserver, want.Nameserver)
		}
		if s.Success != want.Success {
			t.Errorf("row %d: Success=%v, want %v", i, s.Success, want.Success)
		}
		if s.ErrorType != want.ErrorType {
			t.Errorf("row %d: ErrorType=%q, want %q", i, s.ErrorType, want.ErrorType)
		}
	}
}

func TestQueryRange_DifferentNameserver(t *testing.T) {
	store, _ := newTestStore(t)
	defer store.Close()

	ctx := context.Background()
	base := time.Now().UTC()

	_ = store.Record(ctx, DNSMetricSample{Timestamp: base, Nameserver: "10.0.0.1", Domain: "k8s.local", Success: true})
	_ = store.Record(ctx, DNSMetricSample{Timestamp: base, Nameserver: "10.0.0.2", Domain: "k8s.local", Success: true})

	// Query for 10.0.0.1 only.
	rows, err := store.QueryRange(ctx, "10.0.0.1", base.Add(-time.Minute), base.Add(time.Minute))
	if err != nil {
		t.Fatalf("QueryRange: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("QueryRange returned %d rows, want 1", len(rows))
	}
	if rows[0].Nameserver != "10.0.0.1" {
		t.Errorf("unexpected nameserver %q", rows[0].Nameserver)
	}
}

func TestQueryAggregates_HourlyBuckets(t *testing.T) {
	store, _ := newTestStore(t)
	defer store.Close()

	ctx := context.Background()
	ns := "10.96.0.11"

	// Insert samples in two different hours.
	hour1 := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	hour2 := time.Date(2026, 1, 1, 11, 0, 0, 0, time.UTC)

	for i := 0; i < 4; i++ {
		_ = store.Record(ctx, DNSMetricSample{
			Timestamp:  hour1.Add(time.Duration(i) * time.Minute),
			Nameserver: ns,
			Domain:     "k8s.local",
			Success:    i%2 == 0, // alternating success/failure
			LatencyMs:  float64(i + 1),
		})
	}
	for i := 0; i < 2; i++ {
		_ = store.Record(ctx, DNSMetricSample{
			Timestamp:  hour2.Add(time.Duration(i) * time.Minute),
			Nameserver: ns,
			Domain:     "k8s.local",
			Success:    true,
			LatencyMs:  10,
		})
	}

	// Query aggregates over the two-hour range.
	aggs, err := store.QueryAggregates(ctx, ns, "hourly", hour1, hour2.Add(time.Hour))
	if err != nil {
		t.Fatalf("QueryAggregates: %v", err)
	}
	if len(aggs) != 2 {
		t.Fatalf("QueryAggregates returned %d buckets, want 2", len(aggs))
	}

	// Hour 1 bucket: 4 checks, 2 successes.
	h1 := aggs[0]
	if h1.TotalChecks != 4 {
		t.Errorf("hour1 TotalChecks=%d, want 4", h1.TotalChecks)
	}
	if h1.SuccessfulChecks != 2 {
		t.Errorf("hour1 SuccessfulChecks=%d, want 2", h1.SuccessfulChecks)
	}
	wantRate := 2.0 / 4.0
	if diff := h1.SuccessRate - wantRate; diff < -0.001 || diff > 0.001 {
		t.Errorf("hour1 SuccessRate=%.4f, want %.4f", h1.SuccessRate, wantRate)
	}

	// Hour 2 bucket: 2 checks, both success.
	h2 := aggs[1]
	if h2.TotalChecks != 2 {
		t.Errorf("hour2 TotalChecks=%d, want 2", h2.TotalChecks)
	}
	if h2.SuccessfulChecks != 2 {
		t.Errorf("hour2 SuccessfulChecks=%d, want 2", h2.SuccessfulChecks)
	}
}

// TestQueryAggregates_NumericValues verifies that the incremental aggregate computation
// produces correct success_rate and avg_latency_ms values.
// This is a regression test for the SQLite SET-list evaluation-order bug where
// success_rate read the already-mutated success_checks in the DO UPDATE clause.
func TestQueryAggregates_NumericValues(t *testing.T) {
	store, _ := newTestStore(t)
	defer store.Close()

	ctx := context.Background()
	ns := "10.96.0.12"
	hour := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

	// 4 samples: 3 success (latency 10, 20, 30), 1 failure (latency 0).
	records := []DNSMetricSample{
		{Timestamp: hour.Add(0), Nameserver: ns, Domain: "k8s.local", Success: true, LatencyMs: 10},
		{Timestamp: hour.Add(time.Minute), Nameserver: ns, Domain: "k8s.local", Success: true, LatencyMs: 20},
		{Timestamp: hour.Add(2 * time.Minute), Nameserver: ns, Domain: "k8s.local", Success: false, LatencyMs: 0},
		{Timestamp: hour.Add(3 * time.Minute), Nameserver: ns, Domain: "k8s.local", Success: true, LatencyMs: 30},
	}
	for i, r := range records {
		if err := store.Record(ctx, r); err != nil {
			t.Fatalf("Record[%d]: %v", i, err)
		}
	}

	aggs, err := store.QueryAggregates(ctx, ns, "hourly", hour, hour.Add(time.Hour))
	if err != nil {
		t.Fatalf("QueryAggregates: %v", err)
	}
	if len(aggs) != 1 {
		t.Fatalf("expected 1 aggregate bucket, got %d", len(aggs))
	}
	a := aggs[0]

	if a.TotalChecks != 4 {
		t.Errorf("TotalChecks=%d, want 4", a.TotalChecks)
	}
	if a.SuccessfulChecks != 3 {
		t.Errorf("SuccessfulChecks=%d, want 3", a.SuccessfulChecks)
	}

	wantRate := 3.0 / 4.0 // 0.75
	if diff := a.SuccessRate - wantRate; diff < -0.001 || diff > 0.001 {
		t.Errorf("SuccessRate=%.6f, want %.6f (3/4)", a.SuccessRate, wantRate)
	}

	// avg_latency_ms = (10+20+0+30)/4 = 15.0 (all checks including failure)
	wantAvg := (10.0 + 20.0 + 0.0 + 30.0) / 4.0
	if diff := a.AvgLatencyMs - wantAvg; diff < -0.001 || diff > 0.001 {
		t.Errorf("AvgLatencyMs=%.6f, want %.6f", a.AvgLatencyMs, wantAvg)
	}
}

func TestQueryAggregates_InvalidGranularity(t *testing.T) {
	store, _ := newTestStore(t)
	defer store.Close()

	_, err := store.QueryAggregates(context.Background(), "10.0.0.1", "weekly",
		time.Now().Add(-time.Hour), time.Now())
	if err == nil {
		t.Fatal("expected error for invalid granularity, got nil")
	}
}

func TestDeleteOldSamples(t *testing.T) {
	store, _ := newTestStore(t)
	defer store.Close()

	ctx := context.Background()
	ns := "10.96.0.10"
	now := time.Now().UTC()

	// 3 old samples, 2 recent.
	oldSamples := []DNSMetricSample{
		{Timestamp: now.Add(-72 * time.Hour), Nameserver: ns, Domain: "k8s.local", Success: true},
		{Timestamp: now.Add(-48 * time.Hour), Nameserver: ns, Domain: "k8s.local", Success: true},
		{Timestamp: now.Add(-25 * time.Hour), Nameserver: ns, Domain: "k8s.local", Success: false},
	}
	newSamples := []DNSMetricSample{
		{Timestamp: now.Add(-1 * time.Hour), Nameserver: ns, Domain: "k8s.local", Success: true},
		{Timestamp: now, Nameserver: ns, Domain: "k8s.local", Success: true},
	}
	for _, s := range append(oldSamples, newSamples...) {
		if err := store.Record(ctx, s); err != nil {
			t.Fatalf("Record: %v", err)
		}
	}

	cutoff := now.Add(-24 * time.Hour)
	deleted, err := store.DeleteOldSamples(ctx, cutoff)
	if err != nil {
		t.Fatalf("DeleteOldSamples: %v", err)
	}
	if deleted != 3 {
		t.Errorf("DeleteOldSamples returned %d, want 3", deleted)
	}

	// Verify only 2 recent samples remain.
	remaining, err := store.QueryRange(ctx, ns, now.Add(-2*time.Hour), now.Add(time.Hour))
	if err != nil {
		t.Fatalf("QueryRange after delete: %v", err)
	}
	if len(remaining) != 2 {
		t.Errorf("remaining rows=%d, want 2", len(remaining))
	}
}

func TestRunRetention(t *testing.T) {
	store, _ := newTestStore(t)
	defer store.Close()

	// Override retention to 1 day.
	store.cfg.RetentionDays = 1

	ctx := context.Background()
	ns := "10.96.0.10"
	now := time.Now().UTC()

	// Insert one old sample and one recent sample.
	_ = store.Record(ctx, DNSMetricSample{Timestamp: now.Add(-48 * time.Hour), Nameserver: ns, Domain: "k8s.local", Success: true})
	_ = store.Record(ctx, DNSMetricSample{Timestamp: now, Nameserver: ns, Domain: "k8s.local", Success: true})

	n, err := store.RunRetention(ctx)
	if err != nil {
		t.Fatalf("RunRetention: %v", err)
	}
	if n != 1 {
		t.Errorf("RunRetention deleted %d rows, want 1", n)
	}
}

func TestClose_Idempotent(t *testing.T) {
	store, _ := newTestStore(t)
	if err := store.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	// Second close should be a no-op.
	if err := store.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestConcurrentRecord(t *testing.T) {
	store, _ := newTestStore(t)
	defer store.Close()

	ctx := context.Background()
	ns := "10.96.0.10"
	base := time.Now().UTC()

	const workers = 8
	const samplesPerWorker = 25

	var wg sync.WaitGroup
	errs := make(chan error, workers*samplesPerWorker)

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < samplesPerWorker; i++ {
				err := store.Record(ctx, DNSMetricSample{
					Timestamp:  base.Add(time.Duration(w*samplesPerWorker+i) * time.Millisecond),
					Nameserver: ns,
					Domain:     "k8s.local",
					Success:    (i % 3) != 0,
					LatencyMs:  float64(i + 1),
				})
				if err != nil {
					errs <- err
				}
			}
		}(w)
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("concurrent Record error: %v", err)
	}

	// Verify all rows were inserted.
	rows, err := store.QueryRange(ctx, ns, base.Add(-time.Second), base.Add(time.Hour))
	if err != nil {
		t.Fatalf("QueryRange: %v", err)
	}
	want := workers * samplesPerWorker
	if len(rows) != want {
		t.Errorf("expected %d rows, got %d", want, len(rows))
	}
}

func TestDefaultStoragePath(t *testing.T) {
	cfg := &HistoricalMetricsConfig{
		Enabled:     true,
		StoragePath: "", // intentionally empty — should get default
	}
	store, err := NewSQLiteMetricsStore(cfg)
	if err != nil {
		t.Fatalf("NewSQLiteMetricsStore: %v", err)
	}
	if store.cfg.StoragePath != "/var/lib/node-doctor/dns-metrics.db" {
		t.Errorf("StoragePath=%q, want default", store.cfg.StoragePath)
	}
	// Do not initialize (would require the directory to exist); this test only checks defaults.
}

func TestDefaultRetentionDays(t *testing.T) {
	cfg := &HistoricalMetricsConfig{
		Enabled:       true,
		StoragePath:   filepath.Join(t.TempDir(), "test.db"),
		RetentionDays: 0, // should get default of 7
	}
	store, err := NewSQLiteMetricsStore(cfg)
	if err != nil {
		t.Fatalf("NewSQLiteMetricsStore: %v", err)
	}
	if store.cfg.RetentionDays != 7 {
		t.Errorf("RetentionDays=%d, want 7", store.cfg.RetentionDays)
	}
}

// TestQueryAggregates_P95AlwaysZero documents that P95LatencyMs is always 0 because
// no code path currently writes a non-empty stats_json blob. The field is reserved
// for future use; callers should not rely on it until a writer is implemented.
func TestQueryAggregates_P95AlwaysZero(t *testing.T) {
	store, _ := newTestStore(t)
	defer store.Close()

	ctx := context.Background()
	ns := "10.96.0.13"
	hour := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

	_ = store.Record(ctx, DNSMetricSample{Timestamp: hour, Nameserver: ns, Domain: "k8s.local", Success: true, LatencyMs: 100})

	aggs, err := store.QueryAggregates(ctx, ns, "hourly", hour, hour.Add(time.Hour))
	if err != nil {
		t.Fatalf("QueryAggregates: %v", err)
	}
	if len(aggs) == 0 {
		t.Fatal("expected at least one aggregate")
	}
	if aggs[0].P95LatencyMs != 0 {
		t.Errorf("P95LatencyMs=%v, expected 0 (stats_json not yet populated)", aggs[0].P95LatencyMs)
	}
}

func TestQueryRange_EmptyResult(t *testing.T) {
	store, _ := newTestStore(t)
	defer store.Close()

	rows, err := store.QueryRange(context.Background(), "10.0.0.99",
		time.Now().Add(-time.Hour), time.Now())
	if err != nil {
		t.Fatalf("QueryRange: %v", err)
	}
	if rows != nil {
		t.Errorf("expected nil slice for no results, got len=%d", len(rows))
	}
}

func TestRecordBeforeInitialize(t *testing.T) {
	cfg := &HistoricalMetricsConfig{
		Enabled:     true,
		StoragePath: filepath.Join(t.TempDir(), "uninit.db"),
	}
	store, _ := NewSQLiteMetricsStore(cfg)
	// Do NOT call Initialize.
	err := store.Record(context.Background(), DNSMetricSample{
		Timestamp:  time.Now(),
		Nameserver: "10.0.0.1",
		Domain:     "k8s.local",
		Success:    true,
	})
	if err == nil {
		t.Fatal("expected error when recording to uninitialized store, got nil")
	}
}

// TestDatabasePersistence verifies data written in one store instance is readable after re-open.
func TestDatabasePersistence(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "persist.db")
	cfg := &HistoricalMetricsConfig{Enabled: true, StoragePath: dbPath, RetentionDays: 7}

	ns := "10.96.0.10"
	ts := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	// Write with first store instance.
	store1, err := NewSQLiteMetricsStore(cfg)
	if err != nil {
		t.Fatalf("store1 create: %v", err)
	}
	if err := store1.Initialize(context.Background()); err != nil {
		t.Fatalf("store1 init: %v", err)
	}
	if err := store1.Record(context.Background(), DNSMetricSample{
		Timestamp: ts, Nameserver: ns, Domain: "k8s.local", Success: true, LatencyMs: 5,
	}); err != nil {
		t.Fatalf("store1 record: %v", err)
	}
	if err := store1.Close(); err != nil {
		t.Fatalf("store1 close: %v", err)
	}

	// Verify file exists.
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Fatal("database file not created")
	}

	// Read with second store instance.
	store2, err := NewSQLiteMetricsStore(cfg)
	if err != nil {
		t.Fatalf("store2 create: %v", err)
	}
	if err := store2.Initialize(context.Background()); err != nil {
		t.Fatalf("store2 init: %v", err)
	}
	defer store2.Close()

	rows, err := store2.QueryRange(context.Background(), ns, ts.Add(-time.Second), ts.Add(time.Second))
	if err != nil {
		t.Fatalf("store2 query: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row after re-open, got %d", len(rows))
	}
	if !rows[0].Success {
		t.Error("expected Success=true after re-open")
	}
}
