package audit

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStoreListFilteringPaginationMetricsAndReset(t *testing.T) {
	store, err := Open(context.Background(), filepath.Join(t.TempDir(), "audit.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()

	now := time.Now().UTC().Truncate(time.Second)
	events := []Event{
		{Timestamp: now.Add(-3 * time.Hour), Actor: "public", Action: "exchange.request", Method: "POST", Path: "/appcheck/api/v1/exchange", Endpoint: "/api/v1/exchange", StatusCode: 200, Result: ResultSuccess, RequestID: "req-1"},
		{Timestamp: now.Add(-2 * time.Hour), Actor: "public", Action: "exchange.request", Method: "POST", Path: "/appcheck/api/v1/exchange", Endpoint: "/api/v1/exchange", StatusCode: 400, Result: ResultFailure, RequestID: "req-2"},
		{Timestamp: now.Add(-1 * time.Hour), Actor: "public", Action: "verify.request", Method: "GET", Path: "/appcheck/api/v1/verify", Endpoint: "/api/v1/verify", StatusCode: 401, Result: ResultFailure, RequestID: "req-3"},
		{Timestamp: now, Actor: "admin@example.com", Action: "audit.view", Method: "GET", Path: "/appcheck/_admin/api/v1/audit-events", Endpoint: "/_admin/api/v1/audit-events", StatusCode: 200, Result: ResultSuccess},
	}
	for _, event := range events {
		if err := store.Record(context.Background(), event); err != nil {
			t.Fatalf("Record() error = %v", err)
		}
	}

	page, err := store.List(context.Background(), Filter{Action: "exchange.request", Limit: 1, Offset: 1})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if page.Total != 2 || len(page.Items) != 1 || page.Items[0].RequestID != "req-1" {
		t.Fatalf("unexpected page: %#v", page)
	}

	metrics, err := store.Metrics(context.Background(), MetricsFilter{From: now.Add(-4 * time.Hour), To: now.Add(time.Hour), Bucket: time.Hour})
	if err != nil {
		t.Fatalf("Metrics() error = %v", err)
	}
	if metrics.Summary.ExchangeSuccesses != 1 || metrics.Summary.ExchangeFailures != 1 || metrics.Summary.VerifyFailures != 1 || metrics.Summary.Count4xx != 2 {
		t.Fatalf("unexpected summary: %#v", metrics.Summary)
	}

	marker := Event{Actor: "admin@example.com", Action: "audit.reset", Result: ResultSuccess, Message: "Admin reset audit log"}
	if err := store.Reset(context.Background(), marker); err != nil {
		t.Fatalf("Reset() error = %v", err)
	}
	page, err = store.List(context.Background(), Filter{})
	if err != nil {
		t.Fatalf("List() after reset error = %v", err)
	}
	if page.Total != 1 || len(page.Items) != 1 || page.Items[0].Action != "audit.reset" {
		t.Fatalf("reset marker not visible: %#v", page)
	}
}

func TestSafeMetadataDropsSensitiveValues(t *testing.T) {
	store, err := Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()

	if err := store.Record(context.Background(), Event{
		Action: "turnstile.verify",
		Result: ResultFailure,
		Metadata: map[string]any{
			"turnstileToken":      "secret-token",
			"authorizationHeader": "Bearer secret",
			"cookie":              "session=secret",
			"service":             "turnstile",
		},
	}); err != nil {
		t.Fatalf("Record() error = %v", err)
	}
	page, err := store.List(context.Background(), Filter{})
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
