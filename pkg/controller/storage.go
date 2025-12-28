package controller

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	_ "modernc.org/sqlite" // Pure-Go SQLite driver
)

// Storage defines the interface for persistent storage
type Storage interface {
	// Initialize sets up the database and runs migrations
	Initialize(ctx context.Context) error

	// Close closes the database connection
	Close() error

	// Node Reports
	SaveNodeReport(ctx context.Context, report *NodeReport) error
	GetLatestNodeReport(ctx context.Context, nodeName string) (*NodeReport, error)
	GetNodeReports(ctx context.Context, nodeName string, limit int) ([]*NodeReport, error)
	GetAllLatestReports(ctx context.Context) (map[string]*NodeReport, error)
	DeleteOldReports(ctx context.Context, before time.Time) (int64, error)

	// Leases
	SaveLease(ctx context.Context, lease *Lease) error
	GetLease(ctx context.Context, leaseID string) (*Lease, error)
	GetActiveLeases(ctx context.Context) ([]*Lease, error)
	GetNodeLease(ctx context.Context, nodeName string) (*Lease, error)
	UpdateLeaseStatus(ctx context.Context, leaseID, status string) error
	ExpireLeases(ctx context.Context) (int64, error)

	// Correlations
	SaveCorrelation(ctx context.Context, correlation *Correlation) error
	GetCorrelation(ctx context.Context, id string) (*Correlation, error)
	GetActiveCorrelations(ctx context.Context) ([]*Correlation, error)
	UpdateCorrelation(ctx context.Context, correlation *Correlation) error

	// Statistics
	GetNodeCount(ctx context.Context) (int, error)
	GetReportCount(ctx context.Context) (int64, error)
	GetLeaseStats(ctx context.Context) (active, total int64, err error)

	// Maintenance
	RunCleanup(ctx context.Context) error
}

// SQLiteStorage implements Storage using SQLite
type SQLiteStorage struct {
	db        *sql.DB
	dbPath    string
	retention time.Duration
	mu        sync.RWMutex
}

// NewSQLiteStorage creates a new SQLite storage instance
func NewSQLiteStorage(config *StorageConfig) (*SQLiteStorage, error) {
	if config == nil {
		return nil, fmt.Errorf("storage config cannot be nil")
	}

	return &SQLiteStorage{
		dbPath:    config.Path,
		retention: config.Retention,
	}, nil
}

// Initialize opens the database and runs migrations
func (s *SQLiteStorage) Initialize(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Open database
	db, err := sql.Open("sqlite", s.dbPath)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}

	// Configure connection pool
	db.SetMaxOpenConns(1) // SQLite only supports single writer
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0) // Connections don't expire

	// Test connection
	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("failed to ping database: %w", err)
	}

	s.db = db

	// Run migrations
	if err := s.migrate(ctx); err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	log.Printf("[INFO] SQLite storage initialized at %s", s.dbPath)
	return nil
}

// migrate runs database migrations
func (s *SQLiteStorage) migrate(ctx context.Context) error {
	migrations := []string{
		// Node reports table
		`CREATE TABLE IF NOT EXISTS node_reports (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			node_name TEXT NOT NULL,
			node_uid TEXT,
			report_id TEXT,
			timestamp DATETIME NOT NULL,
			overall_health TEXT NOT NULL,
			report_json TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_node_reports_node_name ON node_reports(node_name)`,
		`CREATE INDEX IF NOT EXISTS idx_node_reports_timestamp ON node_reports(timestamp)`,
		`CREATE INDEX IF NOT EXISTS idx_node_reports_node_timestamp ON node_reports(node_name, timestamp DESC)`,

		// Leases table
		`CREATE TABLE IF NOT EXISTS leases (
			id TEXT PRIMARY KEY,
			node_name TEXT NOT NULL,
			remediation_type TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'active',
			reason TEXT,
			granted_at DATETIME NOT NULL,
			expires_at DATETIME NOT NULL,
			completed_at DATETIME,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_leases_node_name ON leases(node_name)`,
		`CREATE INDEX IF NOT EXISTS idx_leases_status ON leases(status)`,
		`CREATE INDEX IF NOT EXISTS idx_leases_expires_at ON leases(expires_at)`,

		// Correlations table
		`CREATE TABLE IF NOT EXISTS correlations (
			id TEXT PRIMARY KEY,
			type TEXT NOT NULL,
			severity TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'active',
			message TEXT,
			confidence REAL DEFAULT 0.0,
			affected_nodes TEXT,
			problem_types TEXT,
			metadata TEXT,
			detected_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL,
			resolved_at DATETIME,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_correlations_status ON correlations(status)`,
		`CREATE INDEX IF NOT EXISTS idx_correlations_detected_at ON correlations(detected_at)`,

		// Events table for audit logging
		`CREATE TABLE IF NOT EXISTS events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			event_type TEXT NOT NULL,
			entity_type TEXT,
			entity_id TEXT,
			node_name TEXT,
			message TEXT,
			metadata TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_events_type ON events(event_type)`,
		`CREATE INDEX IF NOT EXISTS idx_events_node ON events(node_name)`,
		`CREATE INDEX IF NOT EXISTS idx_events_created ON events(created_at)`,

		// Schema version tracking
		`CREATE TABLE IF NOT EXISTS schema_version (
			version INTEGER PRIMARY KEY,
			applied_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
	}

	for i, migration := range migrations {
		if _, err := s.db.ExecContext(ctx, migration); err != nil {
			return fmt.Errorf("migration %d failed: %w", i, err)
		}
	}

	// Record schema version
	_, err := s.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO schema_version (version, applied_at) VALUES (1, CURRENT_TIMESTAMP)`)
	if err != nil {
		return fmt.Errorf("failed to record schema version: %w", err)
	}

	log.Printf("[DEBUG] Database migrations completed")
	return nil
}

// Close closes the database connection
func (s *SQLiteStorage) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

// =====================
// Node Report Operations
// =====================

// SaveNodeReport saves a node report to the database
func (s *SQLiteStorage) SaveNodeReport(ctx context.Context, report *NodeReport) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	reportJSON, err := marshalJSON(report)
	if err != nil {
		return fmt.Errorf("failed to marshal report: %w", err)
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO node_reports (node_name, node_uid, report_id, timestamp, overall_health, report_json)
		VALUES (?, ?, ?, ?, ?, ?)`,
		report.NodeName, report.NodeUID, report.ReportID, report.Timestamp,
		string(report.OverallHealth), reportJSON)

	if err != nil {
		return fmt.Errorf("failed to save node report: %w", err)
	}

	return nil
}

// GetLatestNodeReport returns the most recent report for a node
func (s *SQLiteStorage) GetLatestNodeReport(ctx context.Context, nodeName string) (*NodeReport, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var reportJSON string
	err := s.db.QueryRowContext(ctx, `
		SELECT report_json FROM node_reports
		WHERE node_name = ?
		ORDER BY timestamp DESC
		LIMIT 1`, nodeName).Scan(&reportJSON)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get latest report: %w", err)
	}

	var report NodeReport
	if err := unmarshalJSON(reportJSON, &report); err != nil {
		return nil, fmt.Errorf("failed to unmarshal report: %w", err)
	}

	return &report, nil
}

// GetNodeReports returns recent reports for a node
func (s *SQLiteStorage) GetNodeReports(ctx context.Context, nodeName string, limit int) ([]*NodeReport, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if limit <= 0 {
		limit = 100
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT report_json FROM node_reports
		WHERE node_name = ?
		ORDER BY timestamp DESC
		LIMIT ?`, nodeName, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query reports: %w", err)
	}
	defer rows.Close()

	var reports []*NodeReport
	for rows.Next() {
		var reportJSON string
		if err := rows.Scan(&reportJSON); err != nil {
			return nil, fmt.Errorf("failed to scan report: %w", err)
		}

		var report NodeReport
		if err := unmarshalJSON(reportJSON, &report); err != nil {
			return nil, fmt.Errorf("failed to unmarshal report: %w", err)
		}
		reports = append(reports, &report)
	}

	return reports, rows.Err()
}

// GetAllLatestReports returns the latest report for all nodes
func (s *SQLiteStorage) GetAllLatestReports(ctx context.Context) (map[string]*NodeReport, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.QueryContext(ctx, `
		SELECT node_name, report_json FROM node_reports
		WHERE id IN (
			SELECT MAX(id) FROM node_reports GROUP BY node_name
		)`)
	if err != nil {
		return nil, fmt.Errorf("failed to query reports: %w", err)
	}
	defer rows.Close()

	reports := make(map[string]*NodeReport)
	for rows.Next() {
		var nodeName, reportJSON string
		if err := rows.Scan(&nodeName, &reportJSON); err != nil {
			return nil, fmt.Errorf("failed to scan report: %w", err)
		}

		var report NodeReport
		if err := unmarshalJSON(reportJSON, &report); err != nil {
			return nil, fmt.Errorf("failed to unmarshal report: %w", err)
		}
		reports[nodeName] = &report
	}

	return reports, rows.Err()
}

// DeleteOldReports removes reports older than the given time
func (s *SQLiteStorage) DeleteOldReports(ctx context.Context, before time.Time) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	result, err := s.db.ExecContext(ctx,
		`DELETE FROM node_reports WHERE timestamp < ?`, before)
	if err != nil {
		return 0, fmt.Errorf("failed to delete old reports: %w", err)
	}

	return result.RowsAffected()
}

// =====================
// Lease Operations
// =====================

// SaveLease saves a lease to the database
func (s *SQLiteStorage) SaveLease(ctx context.Context, lease *Lease) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO leases (id, node_name, remediation_type, status, reason, granted_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		lease.ID, lease.NodeName, lease.RemediationType, lease.Status,
		lease.Reason, lease.GrantedAt, lease.ExpiresAt)

	if err != nil {
		return fmt.Errorf("failed to save lease: %w", err)
	}

	return nil
}

// GetLease returns a lease by ID
func (s *SQLiteStorage) GetLease(ctx context.Context, leaseID string) (*Lease, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var lease Lease
	err := s.db.QueryRowContext(ctx, `
		SELECT id, node_name, remediation_type, status, reason, granted_at, expires_at
		FROM leases WHERE id = ?`, leaseID).Scan(
		&lease.ID, &lease.NodeName, &lease.RemediationType, &lease.Status,
		&lease.Reason, &lease.GrantedAt, &lease.ExpiresAt)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get lease: %w", err)
	}

	return &lease, nil
}

// GetActiveLeases returns all active leases
func (s *SQLiteStorage) GetActiveLeases(ctx context.Context) ([]*Lease, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, node_name, remediation_type, status, reason, granted_at, expires_at
		FROM leases
		WHERE status = 'active' AND expires_at > ?
		ORDER BY granted_at`, time.Now())
	if err != nil {
		return nil, fmt.Errorf("failed to query leases: %w", err)
	}
	defer rows.Close()

	var leases []*Lease
	for rows.Next() {
		var lease Lease
		if err := rows.Scan(&lease.ID, &lease.NodeName, &lease.RemediationType,
			&lease.Status, &lease.Reason, &lease.GrantedAt, &lease.ExpiresAt); err != nil {
			return nil, fmt.Errorf("failed to scan lease: %w", err)
		}
		leases = append(leases, &lease)
	}

	return leases, rows.Err()
}

// GetNodeLease returns the active lease for a node, if any
func (s *SQLiteStorage) GetNodeLease(ctx context.Context, nodeName string) (*Lease, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var lease Lease
	err := s.db.QueryRowContext(ctx, `
		SELECT id, node_name, remediation_type, status, reason, granted_at, expires_at
		FROM leases
		WHERE node_name = ? AND status = 'active' AND expires_at > ?
		ORDER BY granted_at DESC
		LIMIT 1`, nodeName, time.Now()).Scan(
		&lease.ID, &lease.NodeName, &lease.RemediationType, &lease.Status,
		&lease.Reason, &lease.GrantedAt, &lease.ExpiresAt)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get node lease: %w", err)
	}

	return &lease, nil
}

// UpdateLeaseStatus updates the status of a lease
func (s *SQLiteStorage) UpdateLeaseStatus(ctx context.Context, leaseID, status string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	completedAt := sql.NullTime{}
	if status == "completed" || status == "expired" || status == "cancelled" {
		completedAt = sql.NullTime{Time: time.Now(), Valid: true}
	}

	_, err := s.db.ExecContext(ctx,
		`UPDATE leases SET status = ?, completed_at = ? WHERE id = ?`,
		status, completedAt, leaseID)

	if err != nil {
		return fmt.Errorf("failed to update lease status: %w", err)
	}

	return nil
}

// ExpireLeases marks expired leases as expired
func (s *SQLiteStorage) ExpireLeases(ctx context.Context) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	result, err := s.db.ExecContext(ctx, `
		UPDATE leases SET status = 'expired', completed_at = ?
		WHERE status = 'active' AND expires_at < ?`,
		time.Now(), time.Now())

	if err != nil {
		return 0, fmt.Errorf("failed to expire leases: %w", err)
	}

	return result.RowsAffected()
}

// =====================
// Correlation Operations
// =====================

// SaveCorrelation saves a correlation to the database
func (s *SQLiteStorage) SaveCorrelation(ctx context.Context, correlation *Correlation) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	affectedNodes, _ := marshalJSON(correlation.AffectedNodes)
	problemTypes, _ := marshalJSON(correlation.ProblemTypes)
	metadata, _ := marshalJSON(correlation.Metadata)

	_, err := s.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO correlations
		(id, type, severity, status, message, confidence, affected_nodes, problem_types, metadata, detected_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		correlation.ID, correlation.Type, correlation.Severity, correlation.Status,
		correlation.Message, correlation.Confidence, affectedNodes, problemTypes,
		metadata, correlation.DetectedAt, correlation.UpdatedAt)

	if err != nil {
		return fmt.Errorf("failed to save correlation: %w", err)
	}

	return nil
}

// GetCorrelation returns a correlation by ID
func (s *SQLiteStorage) GetCorrelation(ctx context.Context, id string) (*Correlation, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var correlation Correlation
	var affectedNodes, problemTypes, metadata string

	err := s.db.QueryRowContext(ctx, `
		SELECT id, type, severity, status, message, confidence, affected_nodes, problem_types, metadata, detected_at, updated_at
		FROM correlations WHERE id = ?`, id).Scan(
		&correlation.ID, &correlation.Type, &correlation.Severity, &correlation.Status,
		&correlation.Message, &correlation.Confidence, &affectedNodes, &problemTypes,
		&metadata, &correlation.DetectedAt, &correlation.UpdatedAt)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get correlation: %w", err)
	}

	unmarshalJSON(affectedNodes, &correlation.AffectedNodes)
	unmarshalJSON(problemTypes, &correlation.ProblemTypes)
	unmarshalJSON(metadata, &correlation.Metadata)

	return &correlation, nil
}

// GetActiveCorrelations returns all active correlations
func (s *SQLiteStorage) GetActiveCorrelations(ctx context.Context) ([]*Correlation, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, type, severity, status, message, confidence, affected_nodes, problem_types, metadata, detected_at, updated_at
		FROM correlations
		WHERE status = 'active'
		ORDER BY detected_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("failed to query correlations: %w", err)
	}
	defer rows.Close()

	var correlations []*Correlation
	for rows.Next() {
		var correlation Correlation
		var affectedNodes, problemTypes, metadata string

		if err := rows.Scan(&correlation.ID, &correlation.Type, &correlation.Severity,
			&correlation.Status, &correlation.Message, &correlation.Confidence,
			&affectedNodes, &problemTypes, &metadata,
			&correlation.DetectedAt, &correlation.UpdatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan correlation: %w", err)
		}

		unmarshalJSON(affectedNodes, &correlation.AffectedNodes)
		unmarshalJSON(problemTypes, &correlation.ProblemTypes)
		unmarshalJSON(metadata, &correlation.Metadata)

		correlations = append(correlations, &correlation)
	}

	return correlations, rows.Err()
}

// UpdateCorrelation updates an existing correlation
func (s *SQLiteStorage) UpdateCorrelation(ctx context.Context, correlation *Correlation) error {
	return s.SaveCorrelation(ctx, correlation)
}

// =====================
// Statistics Operations
// =====================

// GetNodeCount returns the number of unique nodes
func (s *SQLiteStorage) GetNodeCount(ctx context.Context) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var count int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(DISTINCT node_name) FROM node_reports`).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to get node count: %w", err)
	}

	return count, nil
}

// GetReportCount returns the total number of reports
func (s *SQLiteStorage) GetReportCount(ctx context.Context) (int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var count int64
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM node_reports`).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to get report count: %w", err)
	}

	return count, nil
}

// GetLeaseStats returns lease statistics
func (s *SQLiteStorage) GetLeaseStats(ctx context.Context) (active, total int64, err error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM leases`).Scan(&total)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to get total lease count: %w", err)
	}

	err = s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM leases WHERE status = 'active' AND expires_at > ?`,
		time.Now()).Scan(&active)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to get active lease count: %w", err)
	}

	return active, total, nil
}

// =====================
// Cleanup Operations
// =====================

// RunCleanup performs periodic cleanup of old data
func (s *SQLiteStorage) RunCleanup(ctx context.Context) error {
	cutoff := time.Now().Add(-s.retention)

	// Delete old reports
	deleted, err := s.DeleteOldReports(ctx, cutoff)
	if err != nil {
		return fmt.Errorf("failed to cleanup reports: %w", err)
	}
	if deleted > 0 {
		log.Printf("[INFO] Cleaned up %d old reports", deleted)
	}

	// Expire old leases
	expired, err := s.ExpireLeases(ctx)
	if err != nil {
		return fmt.Errorf("failed to expire leases: %w", err)
	}
	if expired > 0 {
		log.Printf("[INFO] Expired %d leases", expired)
	}

	return nil
}

// =====================
// JSON Helpers
// =====================

func marshalJSON(v interface{}) (string, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func unmarshalJSON(data string, v interface{}) error {
	if data == "" {
		return nil
	}
	return json.Unmarshal([]byte(data), v)
}
