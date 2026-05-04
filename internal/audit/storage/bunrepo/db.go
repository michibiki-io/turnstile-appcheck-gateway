package bunrepo

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/mysqldialect"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/dialect/sqlitedialect"
	"github.com/uptrace/bun/driver/pgdriver"
	"github.com/uptrace/bun/driver/sqliteshim"
	"github.com/uptrace/bun/schema"
)

type Config struct {
	StorageType     string
	DSN             string
	SQLitePath      string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	ConnMaxIdleTime time.Duration
}

type Repository struct {
	db      *bun.DB
	sqldb   *sql.DB
	helper  dialectHelper
	nowFunc func() time.Time
}

func Open(ctx context.Context, cfg Config) (*Repository, error) {
	name, err := normalizeStorageType(cfg.StorageType)
	if err != nil {
		return nil, err
	}
	sqldb, dialect, err := openSQLDB(name, cfg)
	if err != nil {
		return nil, err
	}
	applyPool(sqldb, name, cfg)
	db := bun.NewDB(sqldb, dialect)
	repo := &Repository{db: db, sqldb: sqldb, helper: dialectHelper{name: name}, nowFunc: func() time.Time { return time.Now().UTC() }}
	if err := repo.Migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return repo, nil
}

func openSQLDB(name dialectName, cfg Config) (*sql.DB, schema.Dialect, error) {
	switch name {
	case dialectSQLite:
		dsn, err := sqliteDSN(cfg)
		if err != nil {
			return nil, nil, err
		}
		db, err := sql.Open(sqliteshim.ShimName, dsn)
		if err != nil {
			return nil, nil, fmt.Errorf("open audit sqlite database: %w", err)
		}
		return db, sqlitedialect.New(), nil
	case dialectPostgres:
		if strings.TrimSpace(cfg.DSN) == "" {
			return nil, nil, fmt.Errorf("AUDIT_DSN is required for PostgreSQL audit storage")
		}
		db := sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(cfg.DSN)))
		return db, pgdialect.New(), nil
	case dialectMySQL:
		if strings.TrimSpace(cfg.DSN) == "" {
			return nil, nil, fmt.Errorf("AUDIT_DSN is required for MariaDB/MySQL audit storage")
		}
		parsed, err := mysql.ParseDSN(cfg.DSN)
		if err == nil {
			parsed.ParseTime = true
			parsed.Loc = time.UTC
			cfg.DSN = parsed.FormatDSN()
		}
		db, err := sql.Open("mysql", cfg.DSN)
		if err != nil {
			return nil, nil, fmt.Errorf("open audit mysql database: %w", err)
		}
		return db, mysqldialect.New(), nil
	default:
		return nil, nil, fmt.Errorf("unsupported audit storage type %q", name)
	}
}

func sqliteDSN(cfg Config) (string, error) {
	if strings.TrimSpace(cfg.DSN) != "" {
		return cfg.DSN, nil
	}
	path := strings.TrimSpace(cfg.SQLitePath)
	if path == "" {
		return "", fmt.Errorf("AUDIT_SQLITE_PATH is required for SQLite audit storage")
	}
	if path == ":memory:" {
		return "file::memory:?cache=shared", nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", fmt.Errorf("create audit database directory: %w", err)
	}
	if strings.HasPrefix(path, "file:") {
		return path, nil
	}
	return "file:" + path + "?cache=shared&mode=rwc&_journal_mode=WAL&_busy_timeout=5000", nil
}

func applyPool(db *sql.DB, name dialectName, cfg Config) {
	if name == dialectSQLite {
		db.SetMaxOpenConns(1)
		db.SetMaxIdleConns(1)
		db.SetConnMaxLifetime(0)
		return
	}
	if cfg.MaxOpenConns > 0 {
		db.SetMaxOpenConns(cfg.MaxOpenConns)
	}
	if cfg.MaxIdleConns > 0 {
		db.SetMaxIdleConns(cfg.MaxIdleConns)
	}
	if cfg.ConnMaxLifetime > 0 {
		db.SetConnMaxLifetime(cfg.ConnMaxLifetime)
	}
	if cfg.ConnMaxIdleTime > 0 {
		db.SetConnMaxIdleTime(cfg.ConnMaxIdleTime)
	}
}

func (r *Repository) Close() error {
	if r == nil || r.db == nil {
		return nil
	}
	return r.db.Close()
}
