package audit

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

const (
	ResultSuccess = "success"
	ResultFailure = "failure"
	ResultDenied  = "denied"

	PublicGatewayActionFilter = "__public_gateway__"
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

type Event struct {
	Seq         int64          `json:"seq,omitempty"`
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
	From         time.Time
	To           time.Time
	Actor        string
	Action       string
	Endpoint     string
	Path         string
	Method       string
	Result       string
	StatusCode   int
	StatusClass  string
	RequestID    string
	Limit        int
	Cursor       string
	IncludeTotal bool

	// Offset is deprecated and kept only so older callers compile. List uses
	// Cursor as the primary pagination mechanism.
	Offset int
}

type Page struct {
	Items      []Event `json:"items"`
	Total      int     `json:"total,omitempty"`
	NextCursor string  `json:"nextCursor,omitempty"`
	HasNext    bool    `json:"hasNext"`
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

type MetricRollup struct {
	BucketStart time.Time
	BucketSize  time.Duration
	Endpoint    string
	Method      string
	Action      string
	Result      string
	StatusClass string
	StatusCode  int
	Count       int64
}

type Repository interface {
	AppendBatch(context.Context, []Event) error
	AppendMetricRollups(context.Context, []MetricRollup) error
	Get(context.Context, string) (Event, bool, error)
	List(context.Context, Filter) (Page, error)
	Summary(context.Context, Filter) (Summary, error)
	Metrics(context.Context, MetricsFilter) (Metrics, error)
	Reset(context.Context, Event) error
	PruneRetention(context.Context, int) error
}

type AtomicBatchRepository interface {
	AppendAuditBatch(context.Context, []Event, []MetricRollup) error
}

type CloseRepository interface {
	Repository
	Close() error
}

func PrepareEvent(event *Event) error {
	if event == nil {
		return nil
	}
	if event.ID == "" {
		event.ID = "audit_" + randomHex(16)
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	} else {
		event.Timestamp = event.Timestamp.UTC()
	}
	if event.Actor == "" {
		event.Actor = "anonymous"
	}
	if event.Result == "" {
		event.Result = ResultSuccess
	}
	event.Method = strings.ToUpper(strings.TrimSpace(event.Method))
	event.Result = strings.ToLower(strings.TrimSpace(event.Result))
	event.UserAgent = Truncate(event.UserAgent, 512)
	event.Message = Truncate(event.Message, 512)
	event.Metadata = SafeMetadata(event.Metadata)
	return nil
}

func NewMetricRollup(event Event) MetricRollup {
	return MetricRollup{
		BucketStart: event.Timestamp.UTC().Truncate(time.Minute),
		BucketSize:  time.Minute,
		Endpoint:    event.Endpoint,
		Method:      event.Method,
		Action:      event.Action,
		Result:      event.Result,
		StatusClass: statusClass(event.StatusCode),
		StatusCode:  event.StatusCode,
		Count:       1,
	}
}

func statusClass(status int) string {
	if status <= 0 {
		return ""
	}
	return fmt.Sprintf("%dxx", status/100)
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
			out[key] = Truncate(v, 256)
		case fmt.Stringer:
			out[key] = Truncate(v.String(), 256)
		case int, int64, float64, bool, nil:
			out[key] = v
		default:
			out[key] = Truncate(fmt.Sprint(v), 256)
		}
	}
	return out
}

func isSensitiveKey(key string) bool {
	key = strings.ToLower(strings.TrimSpace(key))
	sensitiveParts := []string{
		"password",
		"passwd",
		"secret",
		"token",
		"authorization",
		"cookie",
		"set-cookie",
		"body",
		"payload",
		"credential",
		"service_account",
		"apikey",
		"api_key",
	}
	for _, part := range sensitiveParts {
		if strings.Contains(key, part) {
			return true
		}
	}
	return false
}

func Truncate(value string, max int) string {
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
