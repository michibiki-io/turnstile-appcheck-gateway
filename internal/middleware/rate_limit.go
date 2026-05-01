package middleware

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/example/turnstile-appcheck-gateway/internal/audit"
	"github.com/example/turnstile-appcheck-gateway/internal/config"
)

type rateEntry struct {
	windowStart time.Time
	count       int
}

type fixedWindowLimiter struct {
	mu      sync.Mutex
	window  time.Duration
	entries map[string]rateEntry
}

func NewRateLimiter(cfg config.RateLimitConfig, logger *slog.Logger, recorder audit.Recorder) gin.HandlerFunc {
	limiter := &fixedWindowLimiter{
		window:  cfg.Window,
		entries: map[string]rateEntry{},
	}
	if limiter.window <= 0 {
		limiter.window = time.Minute
	}
	if logger == nil {
		logger = slog.Default()
	}
	return func(c *gin.Context) {
		if !cfg.Enabled || cfg.RequestsPerWindow <= 0 {
			c.Next()
			return
		}
		key := c.ClientIP() + "|" + c.FullPath()
		if !limiter.allow(key, cfg.RequestsPerWindow) {
			recordRateLimit(c.Request.Context(), c, logger, recorder)
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"error": "rate limit exceeded"})
			return
		}
		c.Next()
	}
}

func (l *fixedWindowLimiter) allow(key string, max int) bool {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	entry := l.entries[key]
	if entry.windowStart.IsZero() || now.Sub(entry.windowStart) >= l.window {
		l.entries[key] = rateEntry{windowStart: now, count: 1}
		l.pruneLocked(now)
		return true
	}
	if entry.count >= max {
		return false
	}
	entry.count++
	l.entries[key] = entry
	return true
}

func (l *fixedWindowLimiter) pruneLocked(now time.Time) {
	for key, entry := range l.entries {
		if now.Sub(entry.windowStart) > 2*l.window {
			delete(l.entries, key)
		}
	}
}

func recordRateLimit(ctx context.Context, c *gin.Context, logger *slog.Logger, recorder audit.Recorder) {
	if recorder == nil {
		return
	}
	if err := recorder.Record(ctx, audit.Event{
		Actor:       "public",
		ActorSource: "public",
		Action:      "rate_limit.denied",
		Method:      c.Request.Method,
		Path:        c.Request.URL.Path,
		Endpoint:    c.FullPath(),
		StatusCode:  http.StatusTooManyRequests,
		Result:      audit.ResultDenied,
		RemoteAddr:  c.ClientIP(),
		UserAgent:   c.Request.UserAgent(),
		RequestID:   firstHeader(c, "X-Request-ID", "X-Correlation-ID", "X-Amzn-Trace-Id"),
		ErrorCode:   "rate_limit_exceeded",
		Message:     "Public endpoint rate limit exceeded",
	}); err != nil {
		logger.Warn("failed to record rate limit audit event", "error", err.Error())
	}
}

func firstHeader(c *gin.Context, names ...string) string {
	for _, name := range names {
		if value := strings.TrimSpace(c.GetHeader(name)); value != "" {
			return value
		}
	}
	return ""
}
