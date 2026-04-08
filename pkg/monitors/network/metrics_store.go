// Package network provides network-related health monitoring capabilities.
package network

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	_ "modernc.org/sqlite" // Pure-Go SQLite driver; same driver used by pkg/controller/storage.go
)

// DNSMetricSample is a single per-check DNS measurement recorded to the store.
// One row is persisted per check cycle per nameserver.
type DNSMetricSample struct {
	// Timestamp is when the check completed.
	Timestamp time.Time `json:"timestamp"`

	// Nameserver is the IP address (or host:port) of the queried nameserver.
	Nameserver string `json:"nameserver"`

	// Domain is the domain that was resolved.
	Domain string `json:"domain"`

	// Success indicates whether the resolution succeeded.
	Success bool `json:"success"`

	// LatencyMs is the resolution latency in milliseconds. Zero on failure.
	LatencyMs float64 `json:"latencyMs"`

	// ErrorType is the error classification when Success is false. Empty on success.
	ErrorType string `json:"errorType,omitempty"`
}

// DNSMetricAggregate is an hourly or daily rollup of individual samples.
type DNSMetricAggregate struct {
	// PeriodStart is the start of the aggregation bucket (UTC, truncated to the hour or day).
	PeriodStart time.Time `json:"periodStart"`

	// Granularity is "hourly" or "daily".
	Granularity string `json:"granularity"`

	// Nameserver is the IP address of the nameserver.
	Nameserver string `json:"nameserver"`

	// TotalChecks is the number of raw samples in this bucket.
	TotalChecks int `json:"totalChecks"`

	// SuccessfulChecks is the count of successful resolutions.
	SuccessfulChecks int `json:"successfulChecks"`

	// SuccessRate is SuccessfulChecks / TotalChecks (0–1).
	SuccessRate float64 `json:"successRate"`

	// AvgLatencyMs is the mean resolution latency of successful checks.
	AvgLatencyMs float64 `json:"avgLatencyMs"`

	// P95LatencyMs is the 95th-percentile latency across successful checks.
	P95LatencyMs float64 `json:"p95LatencyMs"`
}

// HistoricalMetricsConfig configures the DNS historical metrics store.
type HistoricalMetricsConfig struct {
	// Enabled activates metric persistence. Disabled by default.
	Enabled bool `json:"enabled"`

	// StoragePath is the absolute path to the SQLite database file.
	// Default: "/var/lib/node-doctor/dns-metrics.db"
	StoragePath string `json:"storagePath"`

	// RetentionDays is the number of days of raw samples to retain before rotation.
	// Default: 7.
	RetentionDays int `json:"retentionDays"`
}

// MetricsStore defines the interface for DNS metric persistence.
// It is designed to be concurrency-safe and self-cleaning.
type MetricsStore interface {
	// Initialize opens (or creates) the underlying database and runs migrations.
	// Must be called before any other method.
	Initialize(ctx context.Context) error

	// Record persists a single DNS check measurement.
	Record(ctx context.Context, sample DNSMetricSample) error

	// QueryRange returns raw samples for a nameserver within [from, to].
	QueryRange(ctx context.Context, nameserver string, from, to time.Time) ([]DNSMetricSample, error)

	// QueryAggregates returns pre-computed hourly or daily rollups.
	// granularity must be "hourly" or "daily".
	QueryAggregates(ctx context.Context, nameserver, granularity string, from, to time.Time) ([]DNSMetricAggregate, error)

	// DeleteOldSamples removes raw samples older than the retention cutoff.
	// Returns the number of rows deleted.
	DeleteOldSamples(ctx context.Context, before time.Time) (int64, error)

	// Close releases the database connection.
	Close() error
}

// SQLiteMetricsStore implements MetricsStore using an embedded SQLite database.
// It is safe for concurrent use; a single writer connection serialises writes
// (matching SQLite's WAL-mode limitation).
type SQLiteMetricsStore struct {
	mu        sync.Mutex
	db        *sql.DB
	cfg       *HistoricalMetricsConfig
}

// NewSQLiteMetricsStore creates a store configured with cfg.
// cfg must be non-nil and Enabled must be true; Initialize must be called before use.
func NewSQLiteMetricsStore(cfg *HistoricalMetricsConfig) (*SQLiteMetricsStore, error) {
	if cfg == nil {
		return nil, fmt.Errorf("HistoricalMetricsConfig must not be nil")
	}
	if cfg.StoragePath == "" {
		cfg.StoragePath = "/var/lib/node-doctor/dns-metrics.db"
	}
	if cfg.RetentionDays <= 0 {
		cfg.RetentionDays = 7
	}
	return &SQLiteMetricsStore{cfg: cfg}, nil
}

// Initialize opens the database and applies schema migrations.
// Calling Initialize more than once returns an error.
func (s *SQLiteMetricsStore) Initialize(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.db != nil {
		return fmt.Errorf("SQLiteMetricsStore already initialized")
	}

	db, err := sql.Open("sqlite", s.cfg.StoragePath)
	if err != nil {
		return fmt.Errorf("open dns metrics db: %w", err)
	}

	// SQLite supports only one writer; cap to 1 open connection.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return fmt.Errorf("ping dns metrics db: %w", err)
	}

	s.db = db

	if err := s.migrate(ctx); err != nil {
		_ = db.Close()
		s.db = nil
		return fmt.Errorf("dns metrics migration: %w", err)
	}

	log.Printf("[INFO] DNS metrics store initialised at %s (retention=%dd)", s.cfg.StoragePath, s.cfg.RetentionDays)
	return nil
}

// migrate creates tables and indexes idempotently.
func (s *SQLiteMetricsStore) migrate(ctx context.Context) error {
	stmts := []string{
		// Enable WAL for better concurrent read performance.
		`PRAGMA journal_mode=WAL`,

		// Raw per-check samples.
		`CREATE TABLE IF NOT EXISTS dns_samples (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			ts          INTEGER NOT NULL,          -- Unix timestamp (seconds)
			nameserver  TEXT    NOT NULL,
			domain      TEXT    NOT NULL,
			success     INTEGER NOT NULL,          -- 1 = success, 0 = failure
			latency_ms  REAL    NOT NULL DEFAULT 0,
			error_type  TEXT    NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX IF NOT EXISTS idx_dns_samples_ns_ts ON dns_samples(nameserver, ts DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_dns_samples_ts    ON dns_samples(ts)`,

		// Hourly and daily pre-computed aggregates.
		// Stored as JSON blobs for flexibility; indexed on (nameserver, granularity, period_start).
		`CREATE TABLE IF NOT EXISTS dns_aggregates (
			id             INTEGER PRIMARY KEY AUTOINCREMENT,
			period_start   INTEGER NOT NULL,  -- Unix timestamp of bucket start (UTC)
			granularity    TEXT    NOT NULL,  -- 'hourly' or 'daily'
			nameserver     TEXT    NOT NULL,
			total_checks   INTEGER NOT NULL,
			success_checks INTEGER NOT NULL,
			success_rate   REAL    NOT NULL,
			avg_latency_ms REAL    NOT NULL DEFAULT 0,
			stats_json     TEXT    NOT NULL DEFAULT '{}'
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_dns_agg_key
			ON dns_aggregates(nameserver, granularity, period_start)`,
	}

	for i, stmt := range stmts {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("migration statement %d: %w", i, err)
		}
	}
	return nil
}

// Record inserts a single DNS check sample and updates hourly/daily aggregates.
func (s *SQLiteMetricsStore) Record(ctx context.Context, sample DNSMetricSample) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.db == nil {
		return fmt.Errorf("SQLiteMetricsStore not initialized")
	}

	successInt := 0
	if sample.Success {
		successInt = 1
	}

	ts := sample.Timestamp.Unix()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO dns_samples (ts, nameserver, domain, success, latency_ms, error_type)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		ts, sample.Nameserver, sample.Domain, successInt, sample.LatencyMs, sample.ErrorType,
	)
	if err != nil {
		return fmt.Errorf("insert dns sample: %w", err)
	}

	// Update hourly aggregate bucket.
	if err := s.upsertAggregate(ctx, sample, "hourly", sample.Timestamp.UTC().Truncate(time.Hour)); err != nil {
		return fmt.Errorf("upsert hourly aggregate: %w", err)
	}

	// Update daily aggregate bucket.
	y, m, d := sample.Timestamp.UTC().Date()
	dayStart := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	if err := s.upsertAggregate(ctx, sample, "daily", dayStart); err != nil {
		return fmt.Errorf("upsert daily aggregate: %w", err)
	}

	return nil
}

// upsertAggregate increments or creates the aggregate bucket for the given period.
// Caller must hold s.mu.
//
// To avoid evaluation-order ambiguity in SQLite's ON CONFLICT DO UPDATE SET list
// (where a later assignment may read a column already mutated by an earlier assignment),
// new aggregate values are computed in Go before the SQL call and passed as parameters.
// This makes the arithmetic auditable in tests rather than buried in SQL.
func (s *SQLiteMetricsStore) upsertAggregate(ctx context.Context, sample DNSMetricSample, granularity string, periodStart time.Time) error {
	ps := periodStart.Unix()
	successInc := 0
	if sample.Success {
		successInc = 1
	}

	// Read the existing bucket so we can compute correct deltas in Go.
	var (
		existingTotal, existingSuccess int
		existingAvgLatency             float64
		exists                         bool
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT total_checks, success_checks, avg_latency_ms
		 FROM dns_aggregates
		 WHERE nameserver = ? AND granularity = ? AND period_start = ?`,
		sample.Nameserver, granularity, ps,
	).Scan(&existingTotal, &existingSuccess, &existingAvgLatency)
	switch {
	case err == nil:
		exists = true
	case isNoRows(err):
		// no row yet; INSERT path below
	default:
		return fmt.Errorf("read existing aggregate: %w", err)
	}

	if !exists {
		// First sample for this bucket — simple INSERT.
		initRate := 0.0
		if successInc == 1 {
			initRate = 1.0
		}
		_, err = s.db.ExecContext(ctx,
			`INSERT INTO dns_aggregates
				(period_start, granularity, nameserver, total_checks, success_checks,
				 success_rate, avg_latency_ms, stats_json)
			 VALUES (?, ?, ?, 1, ?, ?, ?, '{}')`,
			ps, granularity, sample.Nameserver, successInc, initRate, sample.LatencyMs,
		)
		return err
	}

	// Compute new aggregate values in Go to avoid SQL SET-list evaluation-order issues.
	newTotal := existingTotal + 1
	newSuccess := existingSuccess + successInc
	newRate := float64(newSuccess) / float64(newTotal)
	// Running mean: weight existing average by prior sample count, add new observation.
	// Includes zero-latency failure samples in the average (denominator is all checks).
	newAvgLatency := (existingAvgLatency*float64(existingTotal) + sample.LatencyMs) / float64(newTotal)

	_, err = s.db.ExecContext(ctx,
		`UPDATE dns_aggregates
		 SET total_checks   = ?,
		     success_checks = ?,
		     success_rate   = ?,
		     avg_latency_ms = ?
		 WHERE nameserver = ? AND granularity = ? AND period_start = ?`,
		newTotal, newSuccess, newRate, newAvgLatency,
		sample.Nameserver, granularity, ps,
	)
	return err
}

// isNoRows returns true when err is database/sql.ErrNoRows.
func isNoRows(err error) bool {
	return err != nil && err.Error() == "sql: no rows in result set"
}

// QueryRange returns raw samples for nameserver within [from, to], ordered oldest first.
func (s *SQLiteMetricsStore) QueryRange(ctx context.Context, nameserver string, from, to time.Time) ([]DNSMetricSample, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.db == nil {
		return nil, fmt.Errorf("SQLiteMetricsStore not initialized")
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT ts, nameserver, domain, success, latency_ms, error_type
		 FROM dns_samples
		 WHERE nameserver = ? AND ts BETWEEN ? AND ?
		 ORDER BY ts ASC`,
		nameserver, from.Unix(), to.Unix(),
	)
	if err != nil {
		return nil, fmt.Errorf("query dns_samples: %w", err)
	}
	defer rows.Close()

	var samples []DNSMetricSample
	for rows.Next() {
		var (
			ts         int64
			ns, dom    string
			successInt int
			latency    float64
			errType    string
		)
		if err := rows.Scan(&ts, &ns, &dom, &successInt, &latency, &errType); err != nil {
			return nil, fmt.Errorf("scan dns_samples row: %w", err)
		}
		samples = append(samples, DNSMetricSample{
			Timestamp:  time.Unix(ts, 0).UTC(),
			Nameserver: ns,
			Domain:     dom,
			Success:    successInt == 1,
			LatencyMs:  latency,
			ErrorType:  errType,
		})
	}
	return samples, rows.Err()
}

// QueryAggregates returns pre-computed rollups for nameserver and granularity within [from, to].
// granularity must be "hourly" or "daily".
func (s *SQLiteMetricsStore) QueryAggregates(ctx context.Context, nameserver, granularity string, from, to time.Time) ([]DNSMetricAggregate, error) {
	if granularity != "hourly" && granularity != "daily" {
		return nil, fmt.Errorf("granularity must be 'hourly' or 'daily', got %q", granularity)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.db == nil {
		return nil, fmt.Errorf("SQLiteMetricsStore not initialized")
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT period_start, nameserver, total_checks, success_checks, success_rate,
		        avg_latency_ms, stats_json
		 FROM dns_aggregates
		 WHERE nameserver = ? AND granularity = ? AND period_start BETWEEN ? AND ?
		 ORDER BY period_start ASC`,
		nameserver, granularity, from.Unix(), to.Unix(),
	)
	if err != nil {
		return nil, fmt.Errorf("query dns_aggregates: %w", err)
	}
	defer rows.Close()

	var aggs []DNSMetricAggregate
	for rows.Next() {
		var (
			ps                      int64
			ns                      string
			total, success          int
			rate, avgLat            float64
			statsJSON               string
		)
		if err := rows.Scan(&ps, &ns, &total, &success, &rate, &avgLat, &statsJSON); err != nil {
			return nil, fmt.Errorf("scan dns_aggregates row: %w", err)
		}
		// Best-effort decode of p95 from the stats blob.
		p95 := extractP95(statsJSON)
		aggs = append(aggs, DNSMetricAggregate{
			PeriodStart:      time.Unix(ps, 0).UTC(),
			Granularity:      granularity,
			Nameserver:       ns,
			TotalChecks:      total,
			SuccessfulChecks: success,
			SuccessRate:      rate,
			AvgLatencyMs:     avgLat,
			P95LatencyMs:     p95,
		})
	}
	return aggs, rows.Err()
}

// extractP95 parses a p95 value from the stats JSON blob.
// Returns 0 when the field is absent or the JSON is malformed.
func extractP95(statsJSON string) float64 {
	var m map[string]float64
	if err := json.Unmarshal([]byte(statsJSON), &m); err != nil {
		return 0
	}
	return m["p95LatencyMs"]
}

// DeleteOldSamples removes raw samples with ts < before and returns the deleted row count.
func (s *SQLiteMetricsStore) DeleteOldSamples(ctx context.Context, before time.Time) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.db == nil {
		return 0, fmt.Errorf("SQLiteMetricsStore not initialized")
	}

	result, err := s.db.ExecContext(ctx,
		`DELETE FROM dns_samples WHERE ts < ?`, before.Unix())
	if err != nil {
		return 0, fmt.Errorf("delete old dns samples: %w", err)
	}
	n, _ := result.RowsAffected()
	return n, nil
}

// Close releases the database connection.
func (s *SQLiteMetricsStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.db == nil {
		return nil
	}
	err := s.db.Close()
	s.db = nil
	return err
}

// RunRetention deletes raw samples older than cfg.RetentionDays.
// Intended to be called periodically (e.g., once per hour) from the DNS monitor.
func (s *SQLiteMetricsStore) RunRetention(ctx context.Context) (int64, error) {
	cutoff := time.Now().AddDate(0, 0, -s.cfg.RetentionDays)
	n, err := s.DeleteOldSamples(ctx, cutoff)
	if err != nil {
		return 0, err
	}
	if n > 0 {
		log.Printf("[INFO] DNS metrics store: deleted %d samples older than %s", n, cutoff.Format(time.RFC3339))
	}
	return n, nil
}
