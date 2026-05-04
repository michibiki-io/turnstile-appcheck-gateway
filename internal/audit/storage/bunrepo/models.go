package bunrepo

import (
	"time"

	"github.com/uptrace/bun"
)

type auditEventModel struct {
	bun.BaseModel `bun:"table:audit_events"`

	Seq          int64     `bun:"seq,pk,autoincrement"`
	ID           string    `bun:"id,unique,notnull"`
	Timestamp    time.Time `bun:"timestamp,notnull"`
	Actor        string    `bun:"actor"`
	ActorSource  string    `bun:"actor_source"`
	Action       string    `bun:"action,notnull"`
	Method       string    `bun:"method"`
	Path         string    `bun:"path"`
	Endpoint     string    `bun:"endpoint"`
	StatusCode   int       `bun:"status_code"`
	Result       string    `bun:"result,notnull"`
	RemoteAddr   string    `bun:"remote_addr"`
	UserAgent    string    `bun:"user_agent"`
	RequestID    string    `bun:"request_id"`
	DurationMS   int64     `bun:"duration_ms"`
	ErrorCode    string    `bun:"error_code"`
	Message      string    `bun:"message"`
	MetadataJSON string    `bun:"metadata_json"`
}

type metricRollupModel struct {
	bun.BaseModel `bun:"table:audit_metric_rollups"`

	BucketStart time.Time `bun:"bucket_start,pk,notnull"`
	BucketSize  int64     `bun:"bucket_size,pk,notnull"`
	Endpoint    string    `bun:"endpoint,pk,notnull"`
	Method      string    `bun:"method,pk,notnull"`
	Action      string    `bun:"action,pk,notnull"`
	Result      string    `bun:"result,pk,notnull"`
	StatusClass string    `bun:"status_class,pk,notnull"`
	StatusCode  int       `bun:"status_code,pk,notnull"`
	Count       int64     `bun:"count,notnull"`
}
