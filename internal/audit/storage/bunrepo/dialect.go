package bunrepo

import (
	"fmt"
	"strings"
	"time"
)

type dialectName string

const (
	dialectSQLite   dialectName = "sqlite"
	dialectPostgres dialectName = "postgres"
	dialectMySQL    dialectName = "mysql"
)

type dialectHelper struct {
	name dialectName
}

func (h dialectHelper) Name() string {
	return string(h.name)
}

func (h dialectHelper) TimeBucketExpr(column string, bucket time.Duration) string {
	seconds := int64(bucket.Seconds())
	if seconds <= 0 {
		seconds = 3600
	}
	switch h.name {
	case dialectPostgres:
		return fmt.Sprintf("to_char(to_timestamp(floor(extract(epoch from %s) / %d) * %d) AT TIME ZONE 'UTC', 'YYYY-MM-DD\"T\"HH24:MI:SS\"Z\"')", column, seconds, seconds)
	case dialectMySQL:
		return fmt.Sprintf("DATE_FORMAT(FROM_UNIXTIME(FLOOR(UNIX_TIMESTAMP(%s) / %d) * %d), '%%Y-%%m-%%dT%%H:%%i:%%sZ')", column, seconds, seconds)
	default:
		return fmt.Sprintf("strftime('%%Y-%%m-%%dT%%H:%%M:%%SZ', datetime((strftime('%%s', %s) / %d) * %d, 'unixepoch'))", column, seconds, seconds)
	}
}

func (h dialectHelper) NowExpr() string {
	switch h.name {
	case dialectPostgres:
		return "CURRENT_TIMESTAMP"
	case dialectMySQL:
		return "CURRENT_TIMESTAMP"
	default:
		return "CURRENT_TIMESTAMP"
	}
}

func (h dialectHelper) SupportsReturning() bool {
	return h.name == dialectPostgres || h.name == dialectSQLite
}

func normalizeStorageType(value string) (dialectName, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "sqlite":
		return dialectSQLite, nil
	case "postgres", "postgresql":
		return dialectPostgres, nil
	case "mysql", "mariadb":
		return dialectMySQL, nil
	default:
		return "", fmt.Errorf("unsupported audit storage type %q", value)
	}
}
