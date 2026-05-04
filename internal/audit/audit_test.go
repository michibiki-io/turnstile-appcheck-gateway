package audit_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/michibiki-io/turnstile-appcheck-gateway/internal/audit"
	"github.com/michibiki-io/turnstile-appcheck-gateway/internal/audit/storage/bunrepo"
)

func TestStoreListFilteringPaginationMetricsAndReset(t *testing.T) {
	store, err := bunrepo.Open(context.Background(), bunrepo.Config{StorageType: "sqlite", SQLitePath: filepath.Join(t.TempDir(), "audit.db")})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()

	now := time.Now().UTC().Truncate(time.Second)
	events := []audit.Event{
		{Timestamp: now.Add(-3 * time.Hour), Actor: "public", Action: "exchange.request", Method: "POST", Path: "/appcheck/api/v1/exchange", Endpoint: "/api/v1/exchange", StatusCode: 200, Result: audit.ResultSuccess, RequestID: "req-1"},
		{Timestamp: now.Add(-2 * time.Hour), Actor: "public", Action: "exchange.request", Method: "POST", Path: "/appcheck/api/v1/exchange", Endpoint: "/api/v1/exchange", StatusCode: 400, Result: audit.ResultFailure, RequestID: "req-2"},
		{Timestamp: now.Add(-1 * time.Hour), Actor: "public", Action: "verify.request", Method: "GET", Path: "/appcheck/api/v1/verify", Endpoint: "/api/v1/verify", StatusCode: 401, Result: audit.ResultFailure, RequestID: "req-3"},
		{Timestamp: now, Actor: "admin@example.com", Action: "audit.view", Method: "GET", Path: "/appcheck/_admin/api/v1/audit-events", Endpoint: "/_admin/api/v1/audit-events", StatusCode: 200, Result: audit.ResultSuccess},
	}
	for _, event := range events {
		if err := store.AppendBatch(context.Background(), []audit.Event{event}); err != nil {
			t.Fatalf("AppendBatch() error = %v", err)
		}
		if err := store.AppendMetricRollups(context.Background(), []audit.MetricRollup{audit.NewMetricRollup(event)}); err != nil {
			t.Fatalf("AppendMetricRollups() error = %v", err)
		}
	}
	got, ok, err := store.Get(context.Background(), pageID(t, store))
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if !ok || got.Action == "" {
		t.Fatalf("Get() missing event: ok=%v event=%#v", ok, got)
	}

	page, err := store.List(context.Background(), audit.Filter{Action: "exchange.request", Limit: 1, IncludeTotal: true})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if page.Total != 2 || len(page.Items) != 1 || page.Items[0].RequestID != "req-2" || !page.HasNext {
		t.Fatalf("unexpected page: %#v", page)
	}
	next, err := store.List(context.Background(), audit.Filter{Action: "exchange.request", Limit: 1, Cursor: page.NextCursor, IncludeTotal: true})
	if err != nil {
		t.Fatalf("List() next error = %v", err)
	}
	if next.Total != 2 || len(next.Items) != 1 || next.Items[0].RequestID != "req-1" || next.HasNext {
		t.Fatalf("unexpected next page: %#v", next)
	}

	metrics, err := store.Metrics(context.Background(), audit.MetricsFilter{From: now.Add(-4 * time.Hour), To: now.Add(time.Hour), Bucket: time.Hour})
	if err != nil {
		t.Fatalf("Metrics() error = %v", err)
	}
	if metrics.Summary.ExchangeSuccesses != 1 || metrics.Summary.ExchangeFailures != 1 || metrics.Summary.VerifyFailures != 1 || metrics.Summary.Count4xx != 2 {
		t.Fatalf("unexpected summary: %#v", metrics.Summary)
	}

	marker := audit.Event{Actor: "admin@example.com", Action: "audit.reset", Result: audit.ResultSuccess, Message: "Admin reset audit log"}
	if err := store.Reset(context.Background(), marker); err != nil {
		t.Fatalf("Reset() error = %v", err)
	}
	page, err = store.List(context.Background(), audit.Filter{IncludeTotal: true})
	if err != nil {
		t.Fatalf("List() after reset error = %v", err)
	}
	if page.Total != 1 || len(page.Items) != 1 || page.Items[0].Action != "audit.reset" {
		t.Fatalf("reset marker not visible: %#v", page)
	}
}

func TestPruneRetention(t *testing.T) {
	store, err := bunrepo.Open(context.Background(), bunrepo.Config{StorageType: "sqlite", SQLitePath: filepath.Join(t.TempDir(), "audit.db")})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()
	old := audit.Event{Timestamp: time.Now().UTC().AddDate(0, 0, -10), Action: "exchange.request", Result: audit.ResultSuccess}
	recent := audit.Event{Timestamp: time.Now().UTC(), Action: "exchange.request", Result: audit.ResultSuccess}
	if err := store.AppendBatch(context.Background(), []audit.Event{old, recent}); err != nil {
		t.Fatalf("AppendBatch() error = %v", err)
	}
	if err := store.PruneRetention(context.Background(), 1); err != nil {
		t.Fatalf("PruneRetention() error = %v", err)
	}
	page, err := store.List(context.Background(), audit.Filter{IncludeTotal: true})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if page.Total != 1 || len(page.Items) != 1 {
		t.Fatalf("unexpected pruned page: %#v", page)
	}
}

func TestAppendBatchDuplicateIDIsIdempotent(t *testing.T) {
	store, err := bunrepo.Open(context.Background(), bunrepo.Config{StorageType: "sqlite", SQLitePath: filepath.Join(t.TempDir(), "audit.db")})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()

	first := audit.Event{
		ID:         "audit_duplicate_id",
		Timestamp:  time.Date(2026, 5, 5, 1, 2, 3, 0, time.UTC),
		Action:     "exchange.request",
		Method:     "POST",
		Endpoint:   "/api/v1/exchange",
		StatusCode: 200,
		Result:     audit.ResultSuccess,
		RequestID:  "req-original",
	}
	changed := first
	changed.Action = "verify.request"
	changed.Endpoint = "/api/v1/verify"
	changed.StatusCode = 401
	changed.Result = audit.ResultFailure
	changed.RequestID = "req-changed"

	if err := store.AppendBatch(context.Background(), []audit.Event{first}); err != nil {
		t.Fatalf("AppendBatch(first) error = %v", err)
	}
	if err := store.AppendBatch(context.Background(), []audit.Event{changed}); err != nil {
		t.Fatalf("AppendBatch(duplicate) error = %v", err)
	}
	page, err := store.List(context.Background(), audit.Filter{IncludeTotal: true})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if page.Total != 1 || len(page.Items) != 1 {
		t.Fatalf("duplicate id should not create a second row: %#v", page)
	}
	got := page.Items[0]
	if got.Action != first.Action || got.Endpoint != first.Endpoint || got.StatusCode != first.StatusCode || got.RequestID != first.RequestID {
		t.Fatalf("duplicate id should not overwrite original row: %#v", got)
	}
}

func TestAppendAuditBatchPersistsEventsAndRollupsTogether(t *testing.T) {
	store, err := bunrepo.Open(context.Background(), bunrepo.Config{StorageType: "sqlite", SQLitePath: filepath.Join(t.TempDir(), "audit.db")})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()

	now := time.Date(2026, 5, 5, 1, 2, 3, 0, time.UTC)
	event := audit.Event{
		ID:         "audit_atomic_batch",
		Timestamp:  now,
		Action:     "exchange.request",
		Method:     "POST",
		Endpoint:   "/api/v1/exchange",
		StatusCode: 200,
		Result:     audit.ResultSuccess,
	}
	if err := store.AppendAuditBatch(context.Background(), []audit.Event{event}, []audit.MetricRollup{audit.NewMetricRollup(event)}); err != nil {
		t.Fatalf("AppendAuditBatch() error = %v", err)
	}
	if err := store.AppendAuditBatch(context.Background(), []audit.Event{event}, []audit.MetricRollup{audit.NewMetricRollup(event)}); err != nil {
		t.Fatalf("AppendAuditBatch(duplicate retry) error = %v", err)
	}
	page, err := store.List(context.Background(), audit.Filter{IncludeTotal: true})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if page.Total != 1 {
		t.Fatalf("duplicate retry should keep one event row: %#v", page)
	}
	metrics, err := store.Metrics(context.Background(), audit.MetricsFilter{From: now.Add(-time.Minute), To: now.Add(time.Minute), Bucket: time.Minute})
	if err != nil {
		t.Fatalf("Metrics() error = %v", err)
	}
	if metrics.Summary.Total != 2 || metrics.Summary.ExchangeSuccesses != 2 {
		t.Fatalf("retry rollup count should be accumulated exactly once per successful batch call: %#v", metrics.Summary)
	}
}

func pageID(t *testing.T, store *bunrepo.Repository) string {
	t.Helper()
	page, err := store.List(context.Background(), audit.Filter{Limit: 1})
	if err != nil {
		t.Fatalf("List() for id error = %v", err)
	}
	if len(page.Items) == 0 {
		t.Fatal("no audit event found")
	}
	return page.Items[0].ID
}

func TestSafeMetadataDropsSensitiveValues(t *testing.T) {
	store, err := bunrepo.Open(context.Background(), bunrepo.Config{StorageType: "sqlite", SQLitePath: ":memory:"})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()

	if err := store.AppendBatch(context.Background(), []audit.Event{{
		Action: "turnstile.verify",
		Result: audit.ResultFailure,
		Metadata: map[string]any{
			"turnstileToken":      "secret-token",
			"authorizationHeader": "Bearer secret",
			"cookie":              "session=secret",
			"service":             "turnstile",
		},
	}}); err != nil {
		t.Fatalf("AppendBatch() error = %v", err)
	}
	page, err := store.List(context.Background(), audit.Filter{IncludeTotal: true})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	raw := strings.ToLower(strings.TrimSpace(page.Items[0].Metadata["service"].(string)))
	if raw != "turnstile" {
		t.Fatalf("safe metadata missing service: %#v", page.Items[0].Metadata)
	}
	if _, ok := page.Items[0].Metadata["turnstileToken"]; ok {
		t.Fatalf("sensitive token was logged: %#v", page.Items[0].Metadata)
	}
}
