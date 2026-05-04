package audit_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/michibiki-io/turnstile-appcheck-gateway/internal/audit"
	"github.com/michibiki-io/turnstile-appcheck-gateway/internal/audit/storage/bunrepo"
)

func BenchmarkAuditRecorder_Record_VerifySuccessSampledOut(b *testing.B) {
	repo := &benchRepo{}
	rec := audit.NewSyncRecorder(repo, audit.NewPolicy(audit.PolicyConfig{
		PublicMode:                audit.PublicModeFailureStep,
		VerifySuccessSampleRate:   0,
		VerifyFailureSampleRate:   1,
		ExchangeSuccessSampleRate: 1,
		ExchangeFailureSampleRate: 1,
	}))
	event := audit.Event{Action: "verify.request", Result: audit.ResultSuccess, StatusCode: 204, Endpoint: "/api/v1/verify"}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = rec.Record(context.Background(), event)
	}
}

func BenchmarkAuditRecorder_Record_AsyncEnqueue(b *testing.B) {
	repo := &benchRepo{}
	rec := audit.NewAsyncRecorder(repo, audit.NewPolicy(audit.PolicyConfig{
		PublicMode:                audit.PublicModeFailureStep,
		VerifySuccessSampleRate:   1,
		VerifyFailureSampleRate:   1,
		ExchangeSuccessSampleRate: 1,
		ExchangeFailureSampleRate: 1,
	}), audit.AsyncConfig{ChannelSize: b.N + 1, BatchSize: b.N + 1, FlushInterval: time.Hour})
	defer rec.Close(context.Background())
	event := audit.Event{Action: "exchange.request", Result: audit.ResultSuccess, StatusCode: 200, Endpoint: "/api/v1/exchange"}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = rec.Record(context.Background(), event)
	}
}

func BenchmarkAuditPolicy_Decide(b *testing.B) {
	policy := audit.NewPolicy(audit.PolicyConfig{})
	event := audit.Event{Action: "verify.request", Result: audit.ResultSuccess, StatusCode: 204, Endpoint: "/api/v1/verify"}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = policy.Decide(event)
	}
}

func BenchmarkAuditRepository_AppendBatch_SQLite(b *testing.B) {
	repo := openBenchRepo(b)
	events := make([]audit.Event, 100)
	now := time.Now().UTC()
	for i := range events {
		events[i] = audit.Event{Timestamp: now, Action: "exchange.request", Result: audit.ResultSuccess, StatusCode: 200, Endpoint: "/api/v1/exchange"}
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if err := repo.AppendBatch(context.Background(), events); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkAuditRepository_ListKeyset_SQLite(b *testing.B) {
	repo := seedBenchRepo(b, 1000)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := repo.List(context.Background(), audit.Filter{Limit: 50}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkAuditRepository_Summary_SQLite(b *testing.B) {
	repo := seedBenchRepo(b, 1000)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := repo.Summary(context.Background(), audit.Filter{}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkAuditRepository_Metrics_SQLite(b *testing.B) {
	repo := seedBenchRepo(b, 1000)
	now := time.Now().UTC()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := repo.Metrics(context.Background(), audit.MetricsFilter{From: now.Add(-time.Hour), To: now.Add(time.Hour), Bucket: time.Minute}); err != nil {
			b.Fatal(err)
		}
	}
}

func openBenchRepo(b *testing.B) *bunrepo.Repository {
	b.Helper()
	repo, err := bunrepo.Open(context.Background(), bunrepo.Config{StorageType: "sqlite", SQLitePath: filepath.Join(b.TempDir(), "audit.db")})
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = repo.Close() })
	return repo
}

func seedBenchRepo(b *testing.B, n int) *bunrepo.Repository {
	b.Helper()
	repo := openBenchRepo(b)
	now := time.Now().UTC()
	events := make([]audit.Event, 0, n)
	rollups := make([]audit.MetricRollup, 0, n)
	for i := 0; i < n; i++ {
		event := audit.Event{Timestamp: now.Add(time.Duration(i) * time.Second), Action: "verify.request", Result: audit.ResultSuccess, StatusCode: 204, Endpoint: "/api/v1/verify", Method: "GET"}
		events = append(events, event)
		rollups = append(rollups, audit.NewMetricRollup(event))
	}
	if err := repo.AppendBatch(context.Background(), events); err != nil {
		b.Fatal(err)
	}
	if err := repo.AppendMetricRollups(context.Background(), rollups); err != nil {
		b.Fatal(err)
	}
	return repo
}

type benchRepo struct{}

func (benchRepo) AppendBatch(context.Context, []audit.Event) error                { return nil }
func (benchRepo) AppendMetricRollups(context.Context, []audit.MetricRollup) error { return nil }
func (benchRepo) Get(context.Context, string) (audit.Event, bool, error) {
	return audit.Event{}, false, nil
}
func (benchRepo) List(context.Context, audit.Filter) (audit.Page, error) { return audit.Page{}, nil }
func (benchRepo) Summary(context.Context, audit.Filter) (audit.Summary, error) {
	return audit.Summary{}, nil
}
func (benchRepo) Metrics(context.Context, audit.MetricsFilter) (audit.Metrics, error) {
	return audit.Metrics{}, nil
}
func (benchRepo) Reset(context.Context, audit.Event) error  { return nil }
func (benchRepo) PruneRetention(context.Context, int) error { return nil }
