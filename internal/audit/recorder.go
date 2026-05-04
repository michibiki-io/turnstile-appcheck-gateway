package audit

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

type Recorder interface {
	Record(context.Context, Event) error
	Flush(context.Context) error
	Close(context.Context) error
}

type NoopRecorder struct{}

func (NoopRecorder) Record(context.Context, Event) error { return nil }
func (NoopRecorder) Flush(context.Context) error         { return nil }
func (NoopRecorder) Close(context.Context) error         { return nil }

type SyncRecorder struct {
	repo   Repository
	policy *Policy
	stats  RecorderStats
}

func NewSyncRecorder(repo Repository, policy *Policy) *SyncRecorder {
	return &SyncRecorder{repo: repo, policy: policy}
}

func (r *SyncRecorder) Record(ctx context.Context, event Event) error {
	if r == nil || r.repo == nil {
		return nil
	}
	if err := PrepareEvent(&event); err != nil {
		return err
	}
	decision := r.policy.Decide(event)
	if decision.CountMetric {
		if err := r.repo.AppendMetricRollups(ctx, []MetricRollup{NewMetricRollup(event)}); err != nil {
			return err
		}
	}
	if !decision.Persist {
		r.stats.PolicySkippedTotal.Add(1)
		if decision.SampledOut {
			r.stats.SampledOutTotal.Add(1)
		}
		return nil
	}
	r.stats.EnqueueTotal.Add(1)
	return r.repo.AppendBatch(ctx, []Event{event})
}

func (r *SyncRecorder) Flush(context.Context) error { return nil }
func (r *SyncRecorder) Close(context.Context) error { return nil }
func (r *SyncRecorder) AppendBatch(ctx context.Context, events []Event) error {
	return r.repo.AppendBatch(ctx, events)
}
func (r *SyncRecorder) AppendMetricRollups(ctx context.Context, rollups []MetricRollup) error {
	return r.repo.AppendMetricRollups(ctx, rollups)
}
func (r *SyncRecorder) Get(ctx context.Context, id string) (Event, bool, error) {
	return r.repo.Get(ctx, id)
}
func (r *SyncRecorder) List(ctx context.Context, filter Filter) (Page, error) {
	return r.repo.List(ctx, filter)
}
func (r *SyncRecorder) Summary(ctx context.Context, filter Filter) (Summary, error) {
	return r.repo.Summary(ctx, filter)
}
func (r *SyncRecorder) Metrics(ctx context.Context, filter MetricsFilter) (Metrics, error) {
	return r.repo.Metrics(ctx, filter)
}
func (r *SyncRecorder) Reset(ctx context.Context, marker Event) error {
	return r.repo.Reset(ctx, marker)
}
func (r *SyncRecorder) PruneRetention(ctx context.Context, days int) error {
	return r.repo.PruneRetention(ctx, days)
}

type AsyncConfig struct {
	ChannelSize            int
	BatchSize              int
	FlushInterval          time.Duration
	ShutdownFlushTimeout   time.Duration
	DropOnFull             bool
	EnqueueTimeout         time.Duration
	CriticalEnqueueTimeout time.Duration
	RetryMaxAttempts       int
	RetryInitialBackoff    time.Duration
	RetryMaxBackoff        time.Duration
	Logger                 *slog.Logger
}

func DefaultAsyncConfig() AsyncConfig {
	return AsyncConfig{
		ChannelSize:            50000,
		BatchSize:              1000,
		FlushInterval:          100 * time.Millisecond,
		ShutdownFlushTimeout:   5 * time.Second,
		DropOnFull:             true,
		EnqueueTimeout:         5 * time.Millisecond,
		CriticalEnqueueTimeout: 50 * time.Millisecond,
		RetryMaxAttempts:       3,
		RetryInitialBackoff:    100 * time.Millisecond,
		RetryMaxBackoff:        2 * time.Second,
		Logger:                 slog.Default(),
	}
}

type RecorderStats struct {
	EnqueueTotal        atomic.Int64
	EnqueueErrorTotal   atomic.Int64
	DroppedTotal        atomic.Int64
	ChannelFullTotal    atomic.Int64
	FlushTotal          atomic.Int64
	FlushErrorTotal     atomic.Int64
	RetryTotal          atomic.Int64
	RetryExhaustedTotal atomic.Int64
	PolicySkippedTotal  atomic.Int64
	SampledOutTotal     atomic.Int64
	RollupFlushTotal    atomic.Int64
	RollupFlushError    atomic.Int64
}

type asyncItem struct {
	event    Event
	rollup   *MetricRollup
	priority EventPriority
	action   string
}

type flushRequest struct {
	done chan error
}

type AsyncRecorder struct {
	repo     Repository
	policy   *Policy
	cfg      AsyncConfig
	items    chan asyncItem
	flushCh  chan flushRequest
	closeCh  chan context.Context
	doneCh   chan error
	closed   atomic.Bool
	stats    RecorderStats
	closeMux sync.Mutex
}

func NewAsyncRecorder(repo Repository, policy *Policy, cfg AsyncConfig) *AsyncRecorder {
	defaults := DefaultAsyncConfig()
	if cfg.ChannelSize <= 0 {
		cfg.ChannelSize = defaults.ChannelSize
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = defaults.BatchSize
	}
	if cfg.FlushInterval <= 0 {
		cfg.FlushInterval = defaults.FlushInterval
	}
	if cfg.ShutdownFlushTimeout <= 0 {
		cfg.ShutdownFlushTimeout = defaults.ShutdownFlushTimeout
	}
	if cfg.EnqueueTimeout <= 0 {
		cfg.EnqueueTimeout = defaults.EnqueueTimeout
	}
	if cfg.CriticalEnqueueTimeout <= 0 {
		cfg.CriticalEnqueueTimeout = defaults.CriticalEnqueueTimeout
	}
	if cfg.RetryMaxAttempts <= 0 {
		cfg.RetryMaxAttempts = defaults.RetryMaxAttempts
	}
	if cfg.RetryInitialBackoff <= 0 {
		cfg.RetryInitialBackoff = defaults.RetryInitialBackoff
	}
	if cfg.RetryMaxBackoff <= 0 {
		cfg.RetryMaxBackoff = defaults.RetryMaxBackoff
	}
	if cfg.Logger == nil {
		cfg.Logger = defaults.Logger
	}
	r := &AsyncRecorder{
		repo:    repo,
		policy:  policy,
		cfg:     cfg,
		items:   make(chan asyncItem, cfg.ChannelSize),
		flushCh: make(chan flushRequest),
		closeCh: make(chan context.Context, 1),
		doneCh:  make(chan error, 1),
	}
	go r.run()
	return r
}

func (r *AsyncRecorder) Record(ctx context.Context, event Event) error {
	if r == nil || r.repo == nil || r.closed.Load() {
		return nil
	}
	if err := PrepareEvent(&event); err != nil {
		r.stats.EnqueueErrorTotal.Add(1)
		return err
	}
	decision := r.policy.Decide(event)
	if !decision.Persist && !decision.CountMetric {
		r.stats.PolicySkippedTotal.Add(1)
		if decision.SampledOut {
			r.stats.SampledOutTotal.Add(1)
		}
		return nil
	}
	if !decision.Persist {
		r.stats.PolicySkippedTotal.Add(1)
	}
	if decision.SampledOut {
		r.stats.SampledOutTotal.Add(1)
	}
	item := asyncItem{event: event, priority: decision.Priority, action: event.Action}
	if decision.CountMetric {
		rollup := NewMetricRollup(event)
		item.rollup = &rollup
	}
	if !decision.Persist {
		item.event = Event{}
	}
	if err := r.enqueue(ctx, item); err != nil {
		r.stats.EnqueueErrorTotal.Add(1)
		return err
	}
	r.stats.EnqueueTotal.Add(1)
	return nil
}

func (r *AsyncRecorder) enqueue(ctx context.Context, item asyncItem) error {
	select {
	case r.items <- item:
		return nil
	default:
	}
	r.stats.ChannelFullTotal.Add(1)
	timeout := r.cfg.EnqueueTimeout
	if item.priority == EventPriorityCritical {
		timeout = r.cfg.CriticalEnqueueTimeout
	} else if r.cfg.DropOnFull {
		r.drop(item, "channel_full")
		return nil
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case r.items <- item:
		return nil
	case <-ctx.Done():
		r.drop(item, "context_done")
		return ctx.Err()
	case <-timer.C:
		r.drop(item, "enqueue_timeout")
		return nil
	}
}

func (r *AsyncRecorder) drop(item asyncItem, reason string) {
	r.stats.DroppedTotal.Add(1)
	if r.cfg.Logger != nil {
		r.cfg.Logger.Warn("audit event dropped", "reason", reason, "priority", item.priority, "action", item.action)
	}
}

func (r *AsyncRecorder) Flush(ctx context.Context) error {
	if r == nil {
		return nil
	}
	if r.closed.Load() {
		return ErrRecorderClosed
	}
	req := flushRequest{done: make(chan error, 1)}
	select {
	case r.flushCh <- req:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case err := <-req.done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *AsyncRecorder) Close(ctx context.Context) error {
	if r == nil {
		return nil
	}
	if _, ok := ctx.Deadline(); !ok && r.cfg.ShutdownFlushTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, r.cfg.ShutdownFlushTimeout)
		defer cancel()
	}
	r.closeMux.Lock()
	if r.closed.Load() {
		r.closeMux.Unlock()
		select {
		case err := <-r.doneCh:
			return err
		default:
			return nil
		}
	}
	r.closed.Store(true)
	select {
	case r.closeCh <- ctx:
	case <-ctx.Done():
		r.closeMux.Unlock()
		return ctx.Err()
	}
	r.closeMux.Unlock()

	start := time.Now()
	select {
	case err := <-r.doneCh:
		if r.cfg.Logger != nil {
			r.cfg.Logger.Info("audit recorder shutdown complete", "duration_ms", time.Since(start).Milliseconds())
		}
		return err
	case <-ctx.Done():
		if r.cfg.Logger != nil {
			r.cfg.Logger.Error("audit recorder shutdown timeout", "duration_ms", time.Since(start).Milliseconds())
		}
		return ctx.Err()
	}
}

func (r *AsyncRecorder) AppendBatch(ctx context.Context, events []Event) error {
	return r.repo.AppendBatch(ctx, events)
}

func (r *AsyncRecorder) AppendMetricRollups(ctx context.Context, rollups []MetricRollup) error {
	return r.repo.AppendMetricRollups(ctx, rollups)
}

func (r *AsyncRecorder) Get(ctx context.Context, id string) (Event, bool, error) {
	return r.repo.Get(ctx, id)
}

func (r *AsyncRecorder) List(ctx context.Context, filter Filter) (Page, error) {
	return r.repo.List(ctx, filter)
}

func (r *AsyncRecorder) Summary(ctx context.Context, filter Filter) (Summary, error) {
	return r.repo.Summary(ctx, filter)
}

func (r *AsyncRecorder) Metrics(ctx context.Context, filter MetricsFilter) (Metrics, error) {
	return r.repo.Metrics(ctx, filter)
}

func (r *AsyncRecorder) Reset(ctx context.Context, marker Event) error {
	if err := r.Flush(ctx); err != nil {
		return err
	}
	return r.repo.Reset(ctx, marker)
}

func (r *AsyncRecorder) PruneRetention(ctx context.Context, days int) error {
	return r.repo.PruneRetention(ctx, days)
}

func (r *AsyncRecorder) run() {
	ticker := time.NewTicker(r.cfg.FlushInterval)
	defer ticker.Stop()
	var events []Event
	var rollups []MetricRollup
	add := func(item asyncItem) {
		if item.event.ID != "" {
			events = append(events, item.event)
		}
		if item.rollup != nil {
			rollups = append(rollups, *item.rollup)
		}
	}
	drain := func() {
		for {
			select {
			case item := <-r.items:
				add(item)
			default:
				return
			}
		}
	}
	flush := func(ctx context.Context) error {
		if len(events) == 0 && len(rollups) == 0 {
			return nil
		}
		err := r.flushWithRetry(ctx, events, rollups)
		if err == nil {
			events = events[:0]
			rollups = rollups[:0]
		}
		return err
	}
	for {
		select {
		case item := <-r.items:
			add(item)
			if len(events)+len(rollups) >= r.cfg.BatchSize {
				_ = flush(context.Background())
			}
		case req := <-r.flushCh:
			drain()
			req.done <- flush(context.Background())
		case <-ticker.C:
			_ = flush(context.Background())
		case closeCtx := <-r.closeCh:
			var err error
			for {
				select {
				case item := <-r.items:
					add(item)
				default:
					err = flush(closeCtx)
					r.doneCh <- err
					return
				}
			}
		}
	}
}

func (r *AsyncRecorder) flushWithRetry(ctx context.Context, events []Event, rollups []MetricRollup) error {
	var lastErr error
	backoff := r.cfg.RetryInitialBackoff
	for attempt := 1; attempt <= r.cfg.RetryMaxAttempts; attempt++ {
		start := time.Now()
		err := r.flushOnce(ctx, events, rollups)
		if err == nil {
			if r.cfg.Logger != nil {
				r.cfg.Logger.Debug("audit batch flushed", "events", len(events), "rollups", len(rollups), "duration_ms", time.Since(start).Milliseconds())
			}
			return nil
		}
		lastErr = err
		r.stats.RetryTotal.Add(1)
		if attempt == r.cfg.RetryMaxAttempts {
			break
		}
		timer := time.NewTimer(backoff)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		}
		backoff *= 2
		if backoff > r.cfg.RetryMaxBackoff {
			backoff = r.cfg.RetryMaxBackoff
		}
	}
	r.stats.RetryExhaustedTotal.Add(1)
	if r.cfg.Logger != nil {
		r.cfg.Logger.Error("audit batch flush retry exhausted", "error", lastErr, "events", len(events), "rollups", len(rollups))
	}
	return lastErr
}

func (r *AsyncRecorder) flushOnce(ctx context.Context, events []Event, rollups []MetricRollup) error {
	if len(events) > 0 {
		if err := r.repo.AppendBatch(ctx, events); err != nil {
			r.stats.FlushErrorTotal.Add(1)
			return fmt.Errorf("append audit batch: %w", err)
		}
		r.stats.FlushTotal.Add(1)
	}
	if len(rollups) > 0 {
		if err := r.repo.AppendMetricRollups(ctx, rollups); err != nil {
			r.stats.RollupFlushError.Add(1)
			return fmt.Errorf("append audit rollups: %w", err)
		}
		r.stats.RollupFlushTotal.Add(1)
	}
	return nil
}

var ErrRecorderClosed = errors.New("audit recorder is closed")
