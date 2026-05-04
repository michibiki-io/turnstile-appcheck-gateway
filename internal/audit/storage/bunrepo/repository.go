package bunrepo

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/uptrace/bun"

	"github.com/michibiki-io/turnstile-appcheck-gateway/internal/audit"
)

func (r *Repository) AppendBatch(ctx context.Context, events []audit.Event) error {
	if r == nil || r.db == nil || len(events) == 0 {
		return nil
	}
	models := make([]auditEventModel, 0, len(events))
	for _, event := range events {
		if err := audit.PrepareEvent(&event); err != nil {
			return err
		}
		model, err := toEventModel(event)
		if err != nil {
			return err
		}
		models = append(models, model)
	}
	return r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		_, err := tx.NewInsert().Model(&models).Exec(ctx)
		return err
	})
}

func (r *Repository) AppendMetricRollups(ctx context.Context, rollups []audit.MetricRollup) error {
	if r == nil || r.db == nil || len(rollups) == 0 {
		return nil
	}
	merged := mergeRollups(rollups)
	models := make([]metricRollupModel, 0, len(merged))
	for _, rollup := range merged {
		if rollup.Count <= 0 {
			continue
		}
		models = append(models, toRollupModel(rollup))
	}
	if len(models) == 0 {
		return nil
	}
	return r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		query := tx.NewInsert().Model(&models)
		switch r.helper.name {
		case dialectMySQL:
			query = query.On("DUPLICATE KEY UPDATE count = count + VALUES(count)")
		case dialectSQLite:
			query = query.On("CONFLICT (bucket_start, bucket_size, endpoint, method, action, result, status_class, status_code) DO UPDATE").
				Set("count = count + excluded.count")
		default:
			query = query.On("CONFLICT (bucket_start, bucket_size, endpoint, method, action, result, status_class, status_code) DO UPDATE").
				Set("count = audit_metric_rollups.count + EXCLUDED.count")
		}
		_, err := query.Exec(ctx)
		return err
	})
}

func (r *Repository) Get(ctx context.Context, id string) (audit.Event, bool, error) {
	var model auditEventModel
	err := r.db.NewSelect().Model(&model).Where("id = ?", id).Limit(1).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return audit.Event{}, false, nil
	}
	if err != nil {
		return audit.Event{}, false, err
	}
	event, err := fromEventModel(model)
	if err != nil {
		return audit.Event{}, false, err
	}
	return event, true, nil
}

func (r *Repository) List(ctx context.Context, filter audit.Filter) (audit.Page, error) {
	filter = normalizeFilter(filter)
	cursor, err := audit.DecodeCursor(filter.Cursor)
	if err != nil {
		return audit.Page{}, err
	}

	base := r.db.NewSelect().Model((*auditEventModel)(nil))
	applyEventFilters(base, filter)
	if !cursor.Timestamp.IsZero() {
		base.Where("(timestamp < ? OR (timestamp = ? AND seq < ?))", cursor.Timestamp, cursor.Timestamp, cursor.Seq)
	}

	total := 0
	if filter.IncludeTotal {
		countQuery := r.db.NewSelect().Model((*auditEventModel)(nil))
		applyEventFilters(countQuery, filter)
		total, err = countQuery.Count(ctx)
		if err != nil {
			return audit.Page{}, err
		}
	}

	var models []auditEventModel
	base.Order("timestamp DESC", "seq DESC").Limit(filter.Limit + 1)
	if cursor.Timestamp.IsZero() && filter.Offset > 0 {
		base.Offset(filter.Offset)
	}
	if err := base.Scan(ctx, &models); err != nil {
		return audit.Page{}, err
	}
	hasNext := len(models) > filter.Limit
	if hasNext {
		models = models[:filter.Limit]
	}
	items := make([]audit.Event, 0, len(models))
	for _, model := range models {
		event, err := fromEventModel(model)
		if err != nil {
			return audit.Page{}, err
		}
		items = append(items, event)
	}
	next := ""
	if hasNext && len(items) > 0 {
		last := items[len(items)-1]
		next, err = audit.EncodeCursor(audit.Cursor{Timestamp: last.Timestamp, Seq: last.Seq})
		if err != nil {
			return audit.Page{}, err
		}
	}
	return audit.Page{Items: items, Total: total, NextCursor: next, HasNext: hasNext}, nil
}

func (r *Repository) Summary(ctx context.Context, filter audit.Filter) (audit.Summary, error) {
	filter = normalizeFilter(filter)
	query := r.db.NewSelect().Model((*auditEventModel)(nil)).
		ColumnExpr("COUNT(*) AS total").
		ColumnExpr("COALESCE(SUM(CASE WHEN result = ? THEN 1 ELSE 0 END), 0) AS successful", audit.ResultSuccess).
		ColumnExpr("COALESCE(SUM(CASE WHEN result != ? THEN 1 ELSE 0 END), 0) AS failed", audit.ResultSuccess).
		ColumnExpr("COALESCE(SUM(CASE WHEN action = ? AND result = ? THEN 1 ELSE 0 END), 0) AS exchange_successes", "exchange.request", audit.ResultSuccess).
		ColumnExpr("COALESCE(SUM(CASE WHEN action = ? AND result != ? THEN 1 ELSE 0 END), 0) AS exchange_failures", "exchange.request", audit.ResultSuccess).
		ColumnExpr("COALESCE(SUM(CASE WHEN action = ? AND result = ? THEN 1 ELSE 0 END), 0) AS verify_successes", "verify.request", audit.ResultSuccess).
		ColumnExpr("COALESCE(SUM(CASE WHEN action = ? AND result != ? THEN 1 ELSE 0 END), 0) AS verify_failures", "verify.request", audit.ResultSuccess).
		ColumnExpr("COALESCE(SUM(CASE WHEN status_code >= 400 AND status_code < 500 THEN 1 ELSE 0 END), 0) AS count4xx").
		ColumnExpr("COALESCE(SUM(CASE WHEN status_code >= 500 THEN 1 ELSE 0 END), 0) AS count5xx")
	applyEventFilters(query, filter)
	var row struct {
		Total             int `bun:"total"`
		Successful        int `bun:"successful"`
		Failed            int `bun:"failed"`
		ExchangeSuccesses int `bun:"exchange_successes"`
		ExchangeFailures  int `bun:"exchange_failures"`
		VerifySuccesses   int `bun:"verify_successes"`
		VerifyFailures    int `bun:"verify_failures"`
		Count4xx          int `bun:"count4xx"`
		Count5xx          int `bun:"count5xx"`
	}
	if err := query.Scan(ctx, &row); err != nil {
		return audit.Summary{}, err
	}
	return audit.Summary{
		Total:             row.Total,
		Successful:        row.Successful,
		Failed:            row.Failed,
		ExchangeSuccesses: row.ExchangeSuccesses,
		ExchangeFailures:  row.ExchangeFailures,
		VerifySuccesses:   row.VerifySuccesses,
		VerifyFailures:    row.VerifyFailures,
		Count4xx:          row.Count4xx,
		Count5xx:          row.Count5xx,
	}, nil
}

func (r *Repository) Metrics(ctx context.Context, filter audit.MetricsFilter) (audit.Metrics, error) {
	filter = normalizeMetricsFilter(filter, r.nowFunc())
	summary, err := r.rollupSummary(ctx, filter)
	if err != nil {
		return audit.Metrics{}, err
	}
	bucketExpr := r.helper.TimeBucketExpr("bucket_start", filter.Bucket)
	var rows []struct {
		Bucket string `bun:"bucket"`
		Count  int    `bun:"count"`
	}
	query := r.db.NewSelect().Model((*metricRollupModel)(nil)).
		ColumnExpr(bucketExpr+" AS bucket").
		ColumnExpr("COALESCE(SUM(count), 0) AS count").
		Where("bucket_start >= ?", filter.From.UTC()).
		Where("bucket_start <= ?", filter.To.UTC()).
		Where("action IN (?)", bun.In([]string{"gateway.request", "exchange.request", "verify.request", "rate_limit.denied"})).
		GroupExpr("bucket").
		OrderExpr("bucket ASC")
	applyRollupMetricFilters(query, filter)
	if err := query.Scan(ctx, &rows); err != nil {
		return audit.Metrics{}, err
	}

	counts := map[int64]int{}
	for _, row := range rows {
		ts, err := time.Parse(time.RFC3339, row.Bucket)
		if err != nil {
			continue
		}
		counts[ts.UTC().Unix()] = row.Count
	}
	points := make([]audit.MetricPoint, 0)
	for ts := bucketStart(filter.From, filter.Bucket); !ts.After(filter.To); ts = ts.Add(filter.Bucket) {
		points = append(points, audit.MetricPoint{Timestamp: ts, Count: counts[ts.Unix()]})
	}
	return audit.Metrics{From: filter.From, To: filter.To, Bucket: filter.Bucket.String(), Points: points, Summary: summary}, nil
}

func (r *Repository) Reset(ctx context.Context, marker audit.Event) error {
	if err := audit.PrepareEvent(&marker); err != nil {
		return err
	}
	model, err := toEventModel(marker)
	if err != nil {
		return err
	}
	return r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if _, err := tx.NewDelete().Model((*auditEventModel)(nil)).Where("1 = 1").Exec(ctx); err != nil {
			return err
		}
		if _, err := tx.NewDelete().Model((*metricRollupModel)(nil)).Where("1 = 1").Exec(ctx); err != nil {
			return err
		}
		_, err := tx.NewInsert().Model(&model).Exec(ctx)
		return err
	})
}

func (r *Repository) PruneRetention(ctx context.Context, days int) error {
	if days <= 0 {
		return nil
	}
	cutoff := r.nowFunc().AddDate(0, 0, -days)
	return r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if _, err := tx.NewDelete().Model((*auditEventModel)(nil)).Where("timestamp < ?", cutoff).Exec(ctx); err != nil {
			return err
		}
		_, err := tx.NewDelete().Model((*metricRollupModel)(nil)).Where("bucket_start < ?", cutoff).Exec(ctx)
		return err
	})
}

func (r *Repository) rollupSummary(ctx context.Context, filter audit.MetricsFilter) (audit.Summary, error) {
	query := r.db.NewSelect().Model((*metricRollupModel)(nil)).
		ColumnExpr("COALESCE(SUM(count), 0) AS total").
		ColumnExpr("COALESCE(SUM(CASE WHEN result = ? THEN count ELSE 0 END), 0) AS successful", audit.ResultSuccess).
		ColumnExpr("COALESCE(SUM(CASE WHEN result != ? THEN count ELSE 0 END), 0) AS failed", audit.ResultSuccess).
		ColumnExpr("COALESCE(SUM(CASE WHEN action = ? AND result = ? THEN count ELSE 0 END), 0) AS exchange_successes", "exchange.request", audit.ResultSuccess).
		ColumnExpr("COALESCE(SUM(CASE WHEN action = ? AND result != ? THEN count ELSE 0 END), 0) AS exchange_failures", "exchange.request", audit.ResultSuccess).
		ColumnExpr("COALESCE(SUM(CASE WHEN action = ? AND result = ? THEN count ELSE 0 END), 0) AS verify_successes", "verify.request", audit.ResultSuccess).
		ColumnExpr("COALESCE(SUM(CASE WHEN action = ? AND result != ? THEN count ELSE 0 END), 0) AS verify_failures", "verify.request", audit.ResultSuccess).
		ColumnExpr("COALESCE(SUM(CASE WHEN status_code >= 400 AND status_code < 500 THEN count ELSE 0 END), 0) AS count4xx").
		ColumnExpr("COALESCE(SUM(CASE WHEN status_code >= 500 THEN count ELSE 0 END), 0) AS count5xx").
		Where("bucket_start >= ?", filter.From.UTC()).
		Where("bucket_start <= ?", filter.To.UTC()).
		Where("action IN (?)", bun.In([]string{"gateway.request", "exchange.request", "verify.request", "rate_limit.denied"}))
	applyRollupMetricFilters(query, filter)
	var row struct {
		Total             int `bun:"total"`
		Successful        int `bun:"successful"`
		Failed            int `bun:"failed"`
		ExchangeSuccesses int `bun:"exchange_successes"`
		ExchangeFailures  int `bun:"exchange_failures"`
		VerifySuccesses   int `bun:"verify_successes"`
		VerifyFailures    int `bun:"verify_failures"`
		Count4xx          int `bun:"count4xx"`
		Count5xx          int `bun:"count5xx"`
	}
	if err := query.Scan(ctx, &row); err != nil {
		return audit.Summary{}, err
	}
	return audit.Summary{
		Total:             row.Total,
		Successful:        row.Successful,
		Failed:            row.Failed,
		ExchangeSuccesses: row.ExchangeSuccesses,
		ExchangeFailures:  row.ExchangeFailures,
		VerifySuccesses:   row.VerifySuccesses,
		VerifyFailures:    row.VerifyFailures,
		Count4xx:          row.Count4xx,
		Count5xx:          row.Count5xx,
	}, nil
}

func applyEventFilters(query *bun.SelectQuery, filter audit.Filter) {
	if !filter.From.IsZero() {
		query.Where("timestamp >= ?", filter.From.UTC())
	}
	if !filter.To.IsZero() {
		query.Where("timestamp <= ?", filter.To.UTC())
	}
	add := func(column, value string) {
		value = strings.TrimSpace(value)
		if value != "" {
			query.Where(column+" = ?", value)
		}
	}
	add("actor", filter.Actor)
	if filter.Action == audit.PublicGatewayActionFilter {
		query.Where("action IN (?)", bun.In([]string{"gateway.request", "exchange.request", "verify.request"}))
	} else {
		add("action", filter.Action)
	}
	add("endpoint", filter.Endpoint)
	add("method", filter.Method)
	add("result", filter.Result)
	add("request_id", filter.RequestID)
	if strings.TrimSpace(filter.Path) != "" {
		query.Where("path LIKE ?", "%"+strings.TrimSpace(filter.Path)+"%")
	}
	if filter.StatusCode > 0 {
		query.Where("status_code = ?", filter.StatusCode)
	}
	if lo, hi, ok := statusClassRange(filter.StatusClass); ok {
		query.Where("status_code >= ? AND status_code < ?", lo, hi)
	}
}

func applyRollupMetricFilters(query *bun.SelectQuery, filter audit.MetricsFilter) {
	if strings.TrimSpace(filter.Endpoint) != "" {
		query.Where("endpoint = ?", strings.TrimSpace(filter.Endpoint))
	}
	if strings.TrimSpace(filter.Method) != "" {
		query.Where("method = ?", strings.ToUpper(strings.TrimSpace(filter.Method)))
	}
	if strings.TrimSpace(filter.Result) != "" {
		query.Where("result = ?", strings.ToLower(strings.TrimSpace(filter.Result)))
	}
	if strings.TrimSpace(filter.StatusClass) != "" {
		query.Where("status_class = ?", strings.ToLower(strings.TrimSpace(filter.StatusClass)))
	}
}

func normalizeFilter(filter audit.Filter) audit.Filter {
	if filter.Limit <= 0 {
		filter.Limit = 50
	}
	if filter.Limit > 200 {
		filter.Limit = 200
	}
	filter.Method = strings.ToUpper(strings.TrimSpace(filter.Method))
	filter.Result = strings.ToLower(strings.TrimSpace(filter.Result))
	filter.StatusClass = strings.ToLower(strings.TrimSpace(filter.StatusClass))
	return filter
}

func normalizeMetricsFilter(filter audit.MetricsFilter, now time.Time) audit.MetricsFilter {
	if filter.To.IsZero() {
		filter.To = now.UTC()
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

func statusClassRange(value string) (int, int, bool) {
	value = strings.ToLower(strings.TrimSpace(value))
	if prefix, ok := strings.CutSuffix(value, "xx"); ok {
		n, err := strconv.Atoi(prefix)
		if err == nil && n >= 1 && n <= 5 {
			return n * 100, (n + 1) * 100, true
		}
	}
	return 0, 0, false
}

func bucketStart(t time.Time, bucket time.Duration) time.Time {
	return time.Unix(t.UTC().Unix()/int64(bucket.Seconds())*int64(bucket.Seconds()), 0).UTC()
}

func toEventModel(event audit.Event) (auditEventModel, error) {
	metadataJSON := ""
	if len(event.Metadata) > 0 {
		raw, err := json.Marshal(event.Metadata)
		if err != nil {
			return auditEventModel{}, fmt.Errorf("marshal audit metadata: %w", err)
		}
		metadataJSON = string(raw)
	}
	return auditEventModel{
		ID:           event.ID,
		Timestamp:    event.Timestamp.UTC(),
		Actor:        event.Actor,
		ActorSource:  event.ActorSource,
		Action:       event.Action,
		Method:       event.Method,
		Path:         event.Path,
		Endpoint:     event.Endpoint,
		StatusCode:   event.StatusCode,
		Result:       event.Result,
		RemoteAddr:   event.RemoteAddr,
		UserAgent:    audit.Truncate(event.UserAgent, 512),
		RequestID:    event.RequestID,
		DurationMS:   event.DurationMS,
		ErrorCode:    event.ErrorCode,
		Message:      audit.Truncate(event.Message, 512),
		MetadataJSON: metadataJSON,
	}, nil
}

func fromEventModel(model auditEventModel) (audit.Event, error) {
	event := audit.Event{
		Seq:         model.Seq,
		ID:          model.ID,
		Timestamp:   model.Timestamp.UTC(),
		Actor:       model.Actor,
		ActorSource: model.ActorSource,
		Action:      model.Action,
		Method:      model.Method,
		Path:        model.Path,
		Endpoint:    model.Endpoint,
		StatusCode:  model.StatusCode,
		Result:      model.Result,
		RemoteAddr:  model.RemoteAddr,
		UserAgent:   model.UserAgent,
		RequestID:   model.RequestID,
		DurationMS:  model.DurationMS,
		ErrorCode:   model.ErrorCode,
		Message:     model.Message,
	}
	if strings.TrimSpace(model.MetadataJSON) != "" {
		if err := json.Unmarshal([]byte(model.MetadataJSON), &event.Metadata); err != nil {
			return audit.Event{}, err
		}
	}
	return event, nil
}

func toRollupModel(rollup audit.MetricRollup) metricRollupModel {
	return metricRollupModel{
		BucketStart: rollup.BucketStart.UTC(),
		BucketSize:  int64(rollup.BucketSize.Seconds()),
		Endpoint:    strings.TrimSpace(rollup.Endpoint),
		Method:      strings.ToUpper(strings.TrimSpace(rollup.Method)),
		Action:      strings.TrimSpace(rollup.Action),
		Result:      strings.ToLower(strings.TrimSpace(rollup.Result)),
		StatusClass: strings.ToLower(strings.TrimSpace(rollup.StatusClass)),
		StatusCode:  rollup.StatusCode,
		Count:       rollup.Count,
	}
}

func mergeRollups(rollups []audit.MetricRollup) []audit.MetricRollup {
	merged := map[string]audit.MetricRollup{}
	for _, rollup := range rollups {
		if rollup.BucketSize <= 0 {
			rollup.BucketSize = time.Minute
		}
		if rollup.BucketStart.IsZero() {
			rollup.BucketStart = time.Now().UTC().Truncate(rollup.BucketSize)
		}
		if rollup.Count <= 0 {
			rollup.Count = 1
		}
		key := fmt.Sprintf("%s|%s|%s|%s|%s|%s|%d",
			rollup.BucketStart.UTC().Format(time.RFC3339Nano),
			rollup.Endpoint,
			rollup.Method,
			rollup.Action,
			rollup.Result,
			rollup.StatusClass,
			rollup.StatusCode,
		)
		existing := merged[key]
		if existing.Count == 0 {
			merged[key] = rollup
			continue
		}
		existing.Count += rollup.Count
		merged[key] = existing
	}
	out := make([]audit.MetricRollup, 0, len(merged))
	for _, rollup := range merged {
		out = append(out, rollup)
	}
	return out
}
