package audit

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const (
	ResultSuccess = "success"
	ResultFailure = "failure"
	ResultDenied  = "denied"

	publicGatewayActionFilter = "__public_gateway__"
)

var knownActions = []string{
	"gateway.request",
	"exchange.request",
	"verify.request",
	"turnstile.verify",
	"appcheck.exchange",
	"appcheck.verify",
	"forwardauth.denied",
	"rate_limit.denied",
	"admin.dashboard.view",
	"admin.access.denied",
	"admin.metrics.view",
	"audit.view",
	"audit.detail.view",
	"audit.reset",
	"system.startup",
}

func KnownActions() []string {
	return append([]string{}, knownActions...)
}

type Recorder interface {
	Record(context.Context, Event) error
}

type NoopRecorder struct{}

func (NoopRecorder) Record(context.Context, Event) error { return nil }

type Event struct {
	ID          string         `json:"id"`
	Timestamp   time.Time      `json:"timestamp"`
	Actor       string         `json:"actor"`
	ActorSource string         `json:"actorSource"`
	Action      string         `json:"action"`
	Method      string         `json:"method"`
	Path        string         `json:"path"`
	Endpoint    string         `json:"endpoint"`
	StatusCode  int            `json:"statusCode"`
	Result      string         `json:"result"`
	RemoteAddr  string         `json:"remoteAddr"`
	UserAgent   string         `json:"userAgent"`
	RequestID   string         `json:"requestId"`
	DurationMS  int64          `json:"durationMs"`
	ErrorCode   string         `json:"errorCode"`
	Message     string         `json:"message"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

type Filter struct {
	From        time.Time
	To          time.Time
	Actor       string
	Action      string
	Endpoint    string
	Path        string
	Method      string
	Result      string
	StatusCode  int
	StatusClass string
	RequestID   string
	Limit       int
	Offset      int
}

type Page struct {
	Items []Event `json:"items"`
	Total int     `json:"total"`
}

type Summary struct {
	Total             int `json:"total"`
	Successful        int `json:"successful"`
	Failed            int `json:"failed"`
	ExchangeSuccesses int `json:"exchangeSuccesses"`
	ExchangeFailures  int `json:"exchangeFailures"`
	VerifySuccesses   int `json:"verifySuccesses"`
	VerifyFailures    int `json:"verifyFailures"`
	Count4xx          int `json:"count4xx"`
	Count5xx          int `json:"count5xx"`
}

type MetricsFilter struct {
	From        time.Time
	To          time.Time
	Bucket      time.Duration
	Endpoint    string
	Method      string
	Result      string
	StatusClass string
}

type MetricPoint struct {
	Timestamp time.Time `json:"timestamp"`
	Count     int       `json:"count"`
}

type Metrics struct {
	From    time.Time     `json:"from"`
	To      time.Time     `json:"to"`
	Bucket  string        `json:"bucket"`
	Points  []MetricPoint `json:"points"`
	Summary Summary       `json:"summary"`
}

type Store struct {
	db *sql.DB
}

func Open(ctx context.Context, path string) (*Store, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("audit sqlite path is required")
	}
	if path != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return nil, fmt.Errorf("create audit database directory: %w", err)
		}
	}
	dsn := path
	if path != ":memory:" && !strings.Contains(path, "?") {
		dsn += "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open audit sqlite database: %w", err)
	}
	store := &Store{db: db}
	if err := store.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) migrate(ctx context.Context) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS audit_events (
			id TEXT PRIMARY KEY,
			timestamp TEXT NOT NULL,
			actor TEXT,
			actor_source TEXT,
			action TEXT NOT NULL,
			method TEXT,
			path TEXT,
			endpoint TEXT,
			status_code INTEGER,
			result TEXT NOT NULL,
			remote_addr TEXT,
			user_agent TEXT,
			request_id TEXT,
			duration_ms INTEGER,
			error_code TEXT,
			message TEXT,
			metadata_json TEXT
		)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_events_timestamp ON audit_events(timestamp)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_events_actor ON audit_events(actor)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_events_action ON audit_events(action)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_events_endpoint ON audit_events(endpoint)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_events_result ON audit_events(result)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_events_status_code ON audit_events(status_code)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_events_request_id ON audit_events(request_id)`,
	}
	for _, statement := range statements {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("migrate audit database: %w", err)
		}
	}
	return nil
}

func (s *Store) Record(ctx context.Context, event Event) error {
	if s == nil || s.db == nil {
		return nil
	}
	if err := prepareEvent(&event); err != nil {
		return err
	}
	if _, err := insertEvent(ctx, s.db, event); err != nil {
		return fmt.Errorf("insert audit event: %w", err)
	}
	return nil
}

func (s *Store) Reset(ctx context.Context, marker Event) error {
	if s == nil || s.db == nil {
		return nil
	}
	if err := prepareEvent(&marker); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin audit reset: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM audit_events`); err != nil {
		return fmt.Errorf("delete audit events: %w", err)
	}
	if _, err := insertEvent(ctx, tx, marker); err != nil {
		return fmt.Errorf("insert audit reset marker: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit audit reset: %w", err)
	}
	return nil
}

type eventInserter interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func prepareEvent(event *Event) error {
	if event.ID == "" {
		event.ID = "audit_" + randomHex(16)
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}
	if event.Actor == "" {
		event.Actor = "anonymous"
	}
	if event.Result == "" {
		event.Result = ResultSuccess
	}
	event.Metadata = SafeMetadata(event.Metadata)
	return nil
}

func insertEvent(ctx context.Context, target eventInserter, event Event) (sql.Result, error) {
	metadataJSON := ""
	if len(event.Metadata) > 0 {
		raw, err := json.Marshal(event.Metadata)
		if err != nil {
			return nil, fmt.Errorf("marshal audit metadata: %w", err)
		}
		metadataJSON = string(raw)
	}
	return target.ExecContext(ctx, `INSERT INTO audit_events
		(id, timestamp, actor, actor_source, action, method, path, endpoint, status_code, result,
		 remote_addr, user_agent, request_id, duration_ms, error_code, message, metadata_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		event.ID, event.Timestamp.UTC().Format(time.RFC3339Nano), event.Actor, event.ActorSource, event.Action,
		event.Method, event.Path, event.Endpoint, event.StatusCode, event.Result, event.RemoteAddr,
		truncate(event.UserAgent, 512), event.RequestID, event.DurationMS, event.ErrorCode, truncate(event.Message, 512), metadataJSON)
}

func (s *Store) Get(ctx context.Context, id string) (Event, bool, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, timestamp, actor, actor_source, action, method, path, endpoint,
		status_code, result, remote_addr, user_agent, request_id, duration_ms, error_code, message, metadata_json
		FROM audit_events WHERE id = ?`, id)
	event, err := scanEvent(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Event{}, false, nil
	}
	if err != nil {
		return Event{}, false, err
	}
	return event, true, nil
}

func (s *Store) List(ctx context.Context, filter Filter) (Page, error) {
	filter = normalizeFilter(filter)
	where, args := whereClause(filter)
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM audit_events`+where, args...).Scan(&total); err != nil {
		return Page{}, err
	}
	queryArgs := append([]any{}, args...)
	queryArgs = append(queryArgs, filter.Limit, filter.Offset)
	rows, err := s.db.QueryContext(ctx, `SELECT id, timestamp, actor, actor_source, action, method, path, endpoint,
		status_code, result, remote_addr, user_agent, request_id, duration_ms, error_code, message, metadata_json
		FROM audit_events`+where+` ORDER BY timestamp DESC, id DESC LIMIT ? OFFSET ?`, queryArgs...)
	if err != nil {
		return Page{}, err
	}
	defer rows.Close()
	items, err := scanEvents(rows)
	if err != nil {
		return Page{}, err
	}
	return Page{Items: items, Total: total}, nil
}

func (s *Store) Summary(ctx context.Context, filter Filter) (Summary, error) {
	filter.Limit = 1
	filter.Offset = 0
	where, args := whereClause(filter)
	var summary Summary
	err := s.db.QueryRowContext(ctx, `SELECT
		COUNT(*),
		COALESCE(SUM(CASE WHEN result = 'success' THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN result != 'success' THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN action = 'exchange.request' AND result = 'success' THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN action = 'exchange.request' AND result != 'success' THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN action = 'verify.request' AND result = 'success' THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN action = 'verify.request' AND result != 'success' THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN status_code >= 400 AND status_code < 500 THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN status_code >= 500 THEN 1 ELSE 0 END), 0)
		FROM audit_events`+where, args...).Scan(&summary.Total, &summary.Successful, &summary.Failed,
		&summary.ExchangeSuccesses, &summary.ExchangeFailures, &summary.VerifySuccesses, &summary.VerifyFailures,
		&summary.Count4xx, &summary.Count5xx)
	if err != nil {
		return Summary{}, err
	}
	return summary, nil
}

func (s *Store) Metrics(ctx context.Context, filter MetricsFilter) (Metrics, error) {
	filter = normalizeMetricsFilter(filter)
	listFilter := Filter{
		From:        filter.From,
		To:          filter.To,
		Action:      publicGatewayActionFilter,
		Endpoint:    filter.Endpoint,
		Method:      filter.Method,
		Result:      filter.Result,
		StatusClass: filter.StatusClass,
		Limit:       1,
	}
	where, args := whereClause(listFilter)
	rows, err := s.db.QueryContext(ctx, `SELECT timestamp FROM audit_events`+where+` ORDER BY timestamp ASC`, args...)
	if err != nil {
		return Metrics{}, err
	}
	defer rows.Close()

	buckets := map[int64]int{}
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return Metrics{}, err
		}
		ts, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			continue
		}
		key := bucketStart(ts, filter.Bucket).Unix()
		buckets[key]++
	}
	if err := rows.Err(); err != nil {
		return Metrics{}, err
	}
	summary, err := s.Summary(ctx, listFilter)
	if err != nil {
		return Metrics{}, err
	}
	points := make([]MetricPoint, 0)
	for ts := bucketStart(filter.From, filter.Bucket); !ts.After(filter.To); ts = ts.Add(filter.Bucket) {
		points = append(points, MetricPoint{Timestamp: ts, Count: buckets[ts.Unix()]})
	}
	return Metrics{From: filter.From, To: filter.To, Bucket: filter.Bucket.String(), Points: points, Summary: summary}, nil
}

func (s *Store) PruneRetention(ctx context.Context, days int) error {
	if days <= 0 {
		return nil
	}
	cutoff := time.Now().UTC().AddDate(0, 0, -days).Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx, `DELETE FROM audit_events WHERE timestamp < ?`, cutoff)
	return err
}

func SafeMetadata(metadata map[string]any) map[string]any {
	if len(metadata) == 0 {
		return nil
	}
	out := map[string]any{}
	for key, value := range metadata {
		if isSensitiveKey(key) {
			continue
		}
		switch v := value.(type) {
		case string:
			out[key] = truncate(v, 256)
		case fmt.Stringer:
			out[key] = truncate(v.String(), 256)
		case int, int64, float64, bool, nil:
			out[key] = v
		default:
			out[key] = fmt.Sprint(v)
		}
	}
	return out
}

func isSensitiveKey(key string) bool {
	key = strings.ToLower(strings.TrimSpace(key))
	sensitiveParts := []string{"password", "passwd", "secret", "token", "authorization", "cookie", "body", "payload", "credential", "service_account"}
	for _, part := range sensitiveParts {
		if strings.Contains(key, part) {
			return true
		}
	}
	return false
}

func normalizeFilter(filter Filter) Filter {
	if filter.Limit <= 0 {
		filter.Limit = 50
	}
	if filter.Limit > 200 {
		filter.Limit = 200
	}
	if filter.Offset < 0 {
		filter.Offset = 0
	}
	filter.Method = strings.ToUpper(strings.TrimSpace(filter.Method))
	filter.Result = strings.ToLower(strings.TrimSpace(filter.Result))
	filter.StatusClass = strings.ToLower(strings.TrimSpace(filter.StatusClass))
	return filter
}

func normalizeMetricsFilter(filter MetricsFilter) MetricsFilter {
	now := time.Now().UTC()
	if filter.To.IsZero() {
		filter.To = now
	}
	if filter.From.IsZero() {
		filter.From = filter.To.Add(-24 * time.Hour)
	}
	if filter.Bucket <= 0 {
		filter.Bucket = chooseBucket(filter.To.Sub(filter.From))
	}
	filter.From = filter.From.UTC()
	filter.To = filter.To.UTC()
	filter.Method = strings.ToUpper(strings.TrimSpace(filter.Method))
	filter.Result = strings.ToLower(strings.TrimSpace(filter.Result))
	filter.StatusClass = strings.ToLower(strings.TrimSpace(filter.StatusClass))
	return filter
}

func chooseBucket(window time.Duration) time.Duration {
	switch {
	case window <= time.Hour:
		return 5 * time.Minute
	case window <= 6*time.Hour:
		return 15 * time.Minute
	case window <= 24*time.Hour:
		return time.Hour
	case window <= 7*24*time.Hour:
		return 6 * time.Hour
	default:
		return 24 * time.Hour
	}
}

func whereClause(filter Filter) (string, []any) {
	var clauses []string
	var args []any
	add := func(column, value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		clauses = append(clauses, column+" = ?")
		args = append(args, value)
	}
	if !filter.From.IsZero() {
		clauses = append(clauses, "timestamp >= ?")
		args = append(args, filter.From.UTC().Format(time.RFC3339Nano))
	}
	if !filter.To.IsZero() {
		clauses = append(clauses, "timestamp <= ?")
		args = append(args, filter.To.UTC().Format(time.RFC3339Nano))
	}
	add("actor", filter.Actor)
	if filter.Action == publicGatewayActionFilter {
		clauses = append(clauses, "action IN ('gateway.request', 'exchange.request', 'verify.request')")
	} else {
		add("action", filter.Action)
	}
	add("endpoint", filter.Endpoint)
	add("method", filter.Method)
	add("result", filter.Result)
	add("request_id", filter.RequestID)
	if strings.TrimSpace(filter.Path) != "" {
		clauses = append(clauses, "path LIKE ?")
		args = append(args, "%"+strings.TrimSpace(filter.Path)+"%")
	}
	if filter.StatusCode > 0 {
		clauses = append(clauses, "status_code = ?")
		args = append(args, filter.StatusCode)
	}
	if filter.StatusClass != "" {
		if prefix, ok := strings.CutSuffix(filter.StatusClass, "xx"); ok {
			n, err := strconv.Atoi(prefix)
			if err == nil && n >= 1 && n <= 5 {
				clauses = append(clauses, "status_code >= ? AND status_code < ?")
				args = append(args, n*100, (n+1)*100)
			}
		}
	}
	if len(clauses) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(clauses, " AND "), args
}

type scanner interface {
	Scan(dest ...any) error
}

func scanEvent(row scanner) (Event, error) {
	var event Event
	var timestamp, metadataJSON string
	err := row.Scan(&event.ID, &timestamp, &event.Actor, &event.ActorSource, &event.Action, &event.Method, &event.Path,
		&event.Endpoint, &event.StatusCode, &event.Result, &event.RemoteAddr, &event.UserAgent, &event.RequestID,
		&event.DurationMS, &event.ErrorCode, &event.Message, &metadataJSON)
	if err != nil {
		return Event{}, err
	}
	if parsed, err := time.Parse(time.RFC3339Nano, timestamp); err == nil {
		event.Timestamp = parsed
	}
	if metadataJSON != "" {
		_ = json.Unmarshal([]byte(metadataJSON), &event.Metadata)
	}
	return event, nil
}

func scanEvents(rows *sql.Rows) ([]Event, error) {
	var items []Event
	for rows.Next() {
		event, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, event)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func bucketStart(t time.Time, bucket time.Duration) time.Time {
	return time.Unix(t.UTC().Unix()/int64(bucket.Seconds())*int64(bucket.Seconds()), 0).UTC()
}

func truncate(value string, max int) string {
	if max <= 0 || len(value) <= max {
		return value
	}
	return value[:max]
}

func randomHex(bytes int) string {
	buf := make([]byte, bytes)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}
