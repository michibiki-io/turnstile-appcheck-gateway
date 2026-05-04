package audit

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestAsyncRecorderFlushByBatchSize(t *testing.T) {
	repo := &fakeRepo{}
	rec := NewAsyncRecorder(repo, NewPolicy(PolicyConfig{}), AsyncConfig{
		ChannelSize:   10,
		BatchSize:     2,
		FlushInterval: time.Hour,
	})
	defer rec.Close(context.Background())
	if err := rec.Record(context.Background(), Event{Action: "admin.metrics.view", Result: ResultSuccess}); err != nil {
		t.Fatalf("Record() error = %v", err)
	}
	if err := rec.Record(context.Background(), Event{Action: "admin.dashboard.view", Result: ResultSuccess}); err != nil {
		t.Fatalf("Record() error = %v", err)
	}
	if err := rec.Flush(context.Background()); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}
	if got := repo.eventCount(); got != 2 {
		t.Fatalf("event count = %d", got)
	}
}

func TestAsyncRecorderFlushByIntervalAndClose(t *testing.T) {
	repo := &fakeRepo{}
	rec := NewAsyncRecorder(repo, NewPolicy(PolicyConfig{}), AsyncConfig{
		ChannelSize:   10,
		BatchSize:     100,
		FlushInterval: 10 * time.Millisecond,
	})
	if err := rec.Record(context.Background(), Event{Action: "admin.metrics.view", Result: ResultSuccess}); err != nil {
		t.Fatalf("Record() error = %v", err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if repo.eventCount() == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := repo.eventCount(); got != 1 {
		t.Fatalf("interval flush event count = %d", got)
	}
	if err := rec.Record(context.Background(), Event{Action: "admin.dashboard.view", Result: ResultSuccess}); err != nil {
		t.Fatalf("Record() error = %v", err)
	}
	if err := rec.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if got := repo.eventCount(); got != 2 {
		t.Fatalf("close flush event count = %d", got)
	}
}

func TestAsyncRecorderChannelFullDropsBestEffort(t *testing.T) {
	repo := &blockingRepo{block: make(chan struct{})}
	rec := NewAsyncRecorder(repo, NewPolicy(PolicyConfig{VerifySuccessSampleRate: 1}), AsyncConfig{
		ChannelSize:    1,
		BatchSize:      1,
		FlushInterval:  time.Hour,
		DropOnFull:     true,
		EnqueueTimeout: time.Millisecond,
	})
	defer func() {
		close(repo.block)
		_ = rec.Close(context.Background())
	}()
	_ = rec.Record(context.Background(), Event{Action: "verify.request", Result: ResultSuccess, StatusCode: 204, Endpoint: "/api/v1/verify"})
	_ = rec.Record(context.Background(), Event{Action: "verify.request", Result: ResultSuccess, StatusCode: 204, Endpoint: "/api/v1/verify"})
	_ = rec.Record(context.Background(), Event{Action: "verify.request", Result: ResultSuccess, StatusCode: 204, Endpoint: "/api/v1/verify"})
	if rec.stats.DroppedTotal.Load() == 0 {
		t.Fatal("expected at least one dropped best-effort event")
	}
}

func TestAsyncRecorderRetryBehavior(t *testing.T) {
	repo := &fakeRepo{failures: 1}
	rec := NewAsyncRecorder(repo, NewPolicy(PolicyConfig{}), AsyncConfig{
		ChannelSize:         10,
		BatchSize:           10,
		FlushInterval:       time.Hour,
		RetryMaxAttempts:    2,
		RetryInitialBackoff: time.Millisecond,
		RetryMaxBackoff:     time.Millisecond,
	})
	defer rec.Close(context.Background())
	if err := rec.Record(context.Background(), Event{Action: "admin.metrics.view", Result: ResultSuccess}); err != nil {
		t.Fatalf("Record() error = %v", err)
	}
	if err := rec.Flush(context.Background()); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}
	if rec.stats.RetryTotal.Load() == 0 || repo.eventCount() != 1 {
		t.Fatalf("retry did not persist event retries=%d events=%d", rec.stats.RetryTotal.Load(), repo.eventCount())
	}
}

func TestAsyncRecorderRetrySurvivesPartialFlushDuplicateEventID(t *testing.T) {
	repo := newPartialFlushRepo()
	rec := NewAsyncRecorder(repo, NewPolicy(PolicyConfig{}), AsyncConfig{
		ChannelSize:         10,
		BatchSize:           10,
		FlushInterval:       time.Hour,
		RetryMaxAttempts:    2,
		RetryInitialBackoff: time.Millisecond,
		RetryMaxBackoff:     time.Millisecond,
	})
	defer rec.Close(context.Background())

	first := Event{
		ID:         "audit_retry_same_id",
		Timestamp:  time.Date(2026, 5, 5, 1, 2, 3, 0, time.UTC),
		Action:     "exchange.request",
		Method:     "POST",
		Endpoint:   "/api/v1/exchange",
		StatusCode: 200,
		Result:     ResultSuccess,
	}
	if err := rec.Record(context.Background(), first); err != nil {
		t.Fatalf("Record(first) error = %v", err)
	}
	if err := rec.Flush(context.Background()); err != nil {
		t.Fatalf("Flush(first) error = %v", err)
	}
	if got := repo.eventCount(); got != 1 {
		t.Fatalf("event count after retry = %d", got)
	}
	if got := repo.rollupCount(); got != 1 {
		t.Fatalf("rollup count after retry = %d", got)
	}

	second := Event{
		ID:         "audit_retry_next_id",
		Timestamp:  first.Timestamp.Add(time.Minute),
		Action:     "exchange.request",
		Method:     "POST",
		Endpoint:   "/api/v1/exchange",
		StatusCode: 200,
		Result:     ResultSuccess,
	}
	if err := rec.Record(context.Background(), second); err != nil {
		t.Fatalf("Record(second) error = %v", err)
	}
	if err := rec.Flush(context.Background()); err != nil {
		t.Fatalf("Flush(second) error = %v", err)
	}
	if got := repo.eventCount(); got != 2 {
		t.Fatalf("event count after following flush = %d", got)
	}
	if got := repo.rollupCount(); got != 2 {
		t.Fatalf("rollup count after following flush = %d", got)
	}
	if rec.stats.RetryTotal.Load() == 0 {
		t.Fatal("expected retry path to be exercised")
	}
}

type fakeRepo struct {
	mu       sync.Mutex
	events   []Event
	rollups  []MetricRollup
	failures int
}

func (r *fakeRepo) AppendBatch(_ context.Context, events []Event) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.failures > 0 {
		r.failures--
		return errors.New("temporary failure")
	}
	r.events = append(r.events, events...)
	return nil
}

func (r *fakeRepo) AppendMetricRollups(_ context.Context, rollups []MetricRollup) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rollups = append(r.rollups, rollups...)
	return nil
}

func (r *fakeRepo) eventCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.events)
}

func (r *fakeRepo) rollupCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.rollups)
}

func (r *fakeRepo) Get(context.Context, string) (Event, bool, error) { return Event{}, false, nil }
func (r *fakeRepo) List(context.Context, Filter) (Page, error)       { return Page{}, nil }
func (r *fakeRepo) Summary(context.Context, Filter) (Summary, error) { return Summary{}, nil }
func (r *fakeRepo) Metrics(context.Context, MetricsFilter) (Metrics, error) {
	return Metrics{}, nil
}
func (r *fakeRepo) Reset(context.Context, Event) error        { return nil }
func (r *fakeRepo) PruneRetention(context.Context, int) error { return nil }

type blockingRepo struct {
	fakeRepo
	block chan struct{}
}

func (r *blockingRepo) AppendBatch(ctx context.Context, events []Event) error {
	select {
	case <-r.block:
	case <-ctx.Done():
		return ctx.Err()
	}
	return r.fakeRepo.AppendBatch(ctx, events)
}

func (r *blockingRepo) AppendMetricRollups(ctx context.Context, rollups []MetricRollup) error {
	select {
	case <-r.block:
	case <-ctx.Done():
		return ctx.Err()
	}
	return r.fakeRepo.AppendMetricRollups(ctx, rollups)
}

type partialFlushRepo struct {
	mu              sync.Mutex
	events          []Event
	eventsByID      map[string]Event
	rollups         []MetricRollup
	failRollupsOnce bool
}

func newPartialFlushRepo() *partialFlushRepo {
	return &partialFlushRepo{
		eventsByID:      map[string]Event{},
		failRollupsOnce: true,
	}
}

func (r *partialFlushRepo) AppendAuditBatch(_ context.Context, events []Event, rollups []MetricRollup) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, event := range events {
		if existing, ok := r.eventsByID[event.ID]; ok {
			if !sameRetryEvent(existing, event) {
				return errors.New("duplicate audit event id with different payload")
			}
			continue
		}
		r.eventsByID[event.ID] = event
		r.events = append(r.events, event)
	}
	if r.failRollupsOnce {
		r.failRollupsOnce = false
		return errors.New("temporary rollup failure after event insert")
	}
	r.rollups = append(r.rollups, rollups...)
	return nil
}

func (r *partialFlushRepo) AppendBatch(_ context.Context, events []Event) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, event := range events {
		if _, ok := r.eventsByID[event.ID]; ok {
			return errors.New("duplicate audit event id")
		}
		r.eventsByID[event.ID] = event
		r.events = append(r.events, event)
	}
	return nil
}

func (r *partialFlushRepo) AppendMetricRollups(_ context.Context, rollups []MetricRollup) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rollups = append(r.rollups, rollups...)
	return nil
}

func (r *partialFlushRepo) eventCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.events)
}

func (r *partialFlushRepo) rollupCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.rollups)
}

func (r *partialFlushRepo) Get(context.Context, string) (Event, bool, error) {
	return Event{}, false, nil
}
func (r *partialFlushRepo) List(context.Context, Filter) (Page, error) {
	return Page{}, nil
}
func (r *partialFlushRepo) Summary(context.Context, Filter) (Summary, error) {
	return Summary{}, nil
}
func (r *partialFlushRepo) Metrics(context.Context, MetricsFilter) (Metrics, error) {
	return Metrics{}, nil
}
func (r *partialFlushRepo) Reset(context.Context, Event) error        { return nil }
func (r *partialFlushRepo) PruneRetention(context.Context, int) error { return nil }

func sameRetryEvent(a, b Event) bool {
	return a.ID == b.ID &&
		a.Timestamp.Equal(b.Timestamp) &&
		a.Action == b.Action &&
		a.Method == b.Method &&
		a.Endpoint == b.Endpoint &&
		a.StatusCode == b.StatusCode &&
		a.Result == b.Result &&
		a.RequestID == b.RequestID
}
