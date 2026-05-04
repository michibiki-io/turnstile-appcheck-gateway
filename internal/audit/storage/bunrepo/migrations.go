package bunrepo

import (
	"context"
	"fmt"
)

func (r *Repository) Migrate(ctx context.Context) error {
	if err := r.dropLegacyAuditEventsIfNeeded(ctx); err != nil {
		return err
	}
	if _, err := r.db.ExecContext(ctx, `DROP TABLE IF EXISTS audit_events_v2`); err != nil {
		return fmt.Errorf("drop obsolete audit_events_v2 table: %w", err)
	}
	for _, statement := range r.schemaStatements() {
		if _, err := r.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("migrate audit database: %w", err)
		}
	}
	return nil
}

func (r *Repository) schemaStatements() []string {
	switch r.helper.name {
	case dialectPostgres:
		return postgresSchema()
	case dialectMySQL:
		return mysqlSchema()
	default:
		return sqliteSchema()
	}
}

func (r *Repository) dropLegacyAuditEventsIfNeeded(ctx context.Context) error {
	exists, err := r.auditEventsTableExists(ctx)
	if err != nil {
		return fmt.Errorf("inspect audit_events table: %w", err)
	}
	if !exists {
		return nil
	}
	hasSeq, err := r.auditEventsSeqColumnExists(ctx)
	if err != nil {
		return fmt.Errorf("inspect audit_events schema: %w", err)
	}
	if hasSeq {
		return nil
	}
	if _, err := r.db.ExecContext(ctx, `DROP TABLE IF EXISTS audit_events`); err != nil {
		return fmt.Errorf("drop legacy audit_events table: %w", err)
	}
	return nil
}

func (r *Repository) auditEventsTableExists(ctx context.Context) (bool, error) {
	var count int
	var err error
	switch r.helper.name {
	case dialectPostgres:
		err = r.sqldb.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = current_schema() AND table_name = 'audit_events'`).Scan(&count)
	case dialectMySQL:
		err = r.sqldb.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = 'audit_events'`).Scan(&count)
	default:
		err = r.sqldb.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'audit_events'`).Scan(&count)
	}
	return count > 0, err
}

func (r *Repository) auditEventsSeqColumnExists(ctx context.Context) (bool, error) {
	var count int
	var err error
	switch r.helper.name {
	case dialectPostgres:
		err = r.sqldb.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = 'audit_events' AND column_name = 'seq'`).Scan(&count)
	case dialectMySQL:
		err = r.sqldb.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = 'audit_events' AND column_name = 'seq'`).Scan(&count)
	default:
		err = r.sqldb.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info('audit_events') WHERE name = 'seq'`).Scan(&count)
	}
	return count > 0, err
}

func sqliteSchema() []string {
	return []string{
		`CREATE TABLE IF NOT EXISTS audit_events (
			seq INTEGER PRIMARY KEY AUTOINCREMENT,
			id VARCHAR(64) UNIQUE NOT NULL,
			timestamp TIMESTAMP NOT NULL,
			actor VARCHAR(255),
			actor_source VARCHAR(64),
			action VARCHAR(128) NOT NULL,
			method VARCHAR(16),
			path TEXT,
			endpoint VARCHAR(128),
			status_code INTEGER,
			result VARCHAR(32) NOT NULL,
			remote_addr VARCHAR(64),
			user_agent TEXT,
			request_id VARCHAR(128),
			duration_ms BIGINT,
			error_code VARCHAR(128),
			message VARCHAR(512),
			metadata_json TEXT
		)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_events_timestamp_seq ON audit_events(timestamp, seq)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_events_action_timestamp_seq ON audit_events(action, timestamp, seq)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_events_endpoint_timestamp_seq ON audit_events(endpoint, timestamp, seq)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_events_result_timestamp_seq ON audit_events(result, timestamp, seq)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_events_status_timestamp_seq ON audit_events(status_code, timestamp, seq)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_events_request_id ON audit_events(request_id)`,
		rollupSQLiteTable(),
		`CREATE INDEX IF NOT EXISTS idx_audit_metric_rollups_bucket ON audit_metric_rollups(bucket_start)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_metric_rollups_filter_bucket ON audit_metric_rollups(endpoint, method, result, status_class, bucket_start)`,
	}
}

func postgresSchema() []string {
	return []string{
		`CREATE TABLE IF NOT EXISTS audit_events (
			seq BIGSERIAL PRIMARY KEY,
			id VARCHAR(64) UNIQUE NOT NULL,
			timestamp TIMESTAMPTZ NOT NULL,
			actor VARCHAR(255),
			actor_source VARCHAR(64),
			action VARCHAR(128) NOT NULL,
			method VARCHAR(16),
			path TEXT,
			endpoint VARCHAR(128),
			status_code INTEGER,
			result VARCHAR(32) NOT NULL,
			remote_addr VARCHAR(64),
			user_agent TEXT,
			request_id VARCHAR(128),
			duration_ms BIGINT,
			error_code VARCHAR(128),
			message VARCHAR(512),
			metadata_json TEXT
		)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_events_timestamp_seq ON audit_events(timestamp, seq)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_events_action_timestamp_seq ON audit_events(action, timestamp, seq)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_events_endpoint_timestamp_seq ON audit_events(endpoint, timestamp, seq)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_events_result_timestamp_seq ON audit_events(result, timestamp, seq)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_events_status_timestamp_seq ON audit_events(status_code, timestamp, seq)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_events_request_id ON audit_events(request_id)`,
		rollupPostgresTable(),
		`CREATE INDEX IF NOT EXISTS idx_audit_metric_rollups_bucket ON audit_metric_rollups(bucket_start)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_metric_rollups_filter_bucket ON audit_metric_rollups(endpoint, method, result, status_class, bucket_start)`,
	}
}

func mysqlSchema() []string {
	return []string{
		`CREATE TABLE IF NOT EXISTS audit_events (
			seq BIGINT AUTO_INCREMENT PRIMARY KEY,
			id VARCHAR(64) UNIQUE NOT NULL,
			timestamp DATETIME(6) NOT NULL,
			actor VARCHAR(255),
			actor_source VARCHAR(64),
			action VARCHAR(128) NOT NULL,
			method VARCHAR(16),
			path TEXT,
			endpoint VARCHAR(128),
			status_code INTEGER,
			result VARCHAR(32) NOT NULL,
			remote_addr VARCHAR(64),
			user_agent TEXT,
			request_id VARCHAR(128),
			duration_ms BIGINT,
			error_code VARCHAR(128),
			message VARCHAR(512),
			metadata_json TEXT,
			INDEX idx_audit_events_timestamp_seq (timestamp, seq),
			INDEX idx_audit_events_action_timestamp_seq (action, timestamp, seq),
			INDEX idx_audit_events_endpoint_timestamp_seq (endpoint, timestamp, seq),
			INDEX idx_audit_events_result_timestamp_seq (result, timestamp, seq),
			INDEX idx_audit_events_status_timestamp_seq (status_code, timestamp, seq),
			INDEX idx_audit_events_request_id (request_id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,
		rollupMySQLTable(),
	}
}

func rollupSQLiteTable() string {
	return `CREATE TABLE IF NOT EXISTS audit_metric_rollups (
		bucket_start TIMESTAMP NOT NULL,
		bucket_size BIGINT NOT NULL,
		endpoint VARCHAR(128) NOT NULL,
		method VARCHAR(16) NOT NULL,
		action VARCHAR(128) NOT NULL,
		result VARCHAR(32) NOT NULL,
		status_class VARCHAR(8) NOT NULL,
		status_code INTEGER NOT NULL,
		count BIGINT NOT NULL,
		PRIMARY KEY (bucket_start, bucket_size, endpoint, method, action, result, status_class, status_code)
	)`
}

func rollupPostgresTable() string {
	return `CREATE TABLE IF NOT EXISTS audit_metric_rollups (
		bucket_start TIMESTAMPTZ NOT NULL,
		bucket_size BIGINT NOT NULL,
		endpoint VARCHAR(128) NOT NULL,
		method VARCHAR(16) NOT NULL,
		action VARCHAR(128) NOT NULL,
		result VARCHAR(32) NOT NULL,
		status_class VARCHAR(8) NOT NULL,
		status_code INTEGER NOT NULL,
		count BIGINT NOT NULL,
		PRIMARY KEY (bucket_start, bucket_size, endpoint, method, action, result, status_class, status_code)
	)`
}

func rollupMySQLTable() string {
	return `CREATE TABLE IF NOT EXISTS audit_metric_rollups (
		bucket_start DATETIME(6) NOT NULL,
		bucket_size BIGINT NOT NULL,
		endpoint VARCHAR(128) NOT NULL,
		method VARCHAR(16) NOT NULL,
		action VARCHAR(128) NOT NULL,
		result VARCHAR(32) NOT NULL,
		status_class VARCHAR(8) NOT NULL,
		status_code INTEGER NOT NULL,
		count BIGINT NOT NULL,
		PRIMARY KEY (bucket_start, bucket_size, endpoint, method, action, result, status_class, status_code),
		INDEX idx_audit_metric_rollups_bucket (bucket_start),
		INDEX idx_audit_metric_rollups_filter_bucket (endpoint, method, result, status_class, bucket_start)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`
}
