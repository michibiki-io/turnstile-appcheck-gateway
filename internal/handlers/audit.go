package handlers

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/example/turnstile-appcheck-gateway/internal/audit"
)

func recordAudit(ctx context.Context, logger *slog.Logger, recorder audit.Recorder, event audit.Event) {
	if recorder == nil {
		return
	}
	if err := recorder.Record(ctx, event); err != nil && logger != nil {
		logger.Warn("failed to record audit event", "error", err.Error())
	}
}

func requestAuditEvent(c *gin.Context, start time.Time, action, message string) audit.Event {
	status := c.Writer.Status()
	result := audit.ResultSuccess
	if status >= http.StatusBadRequest {
		result = audit.ResultFailure
	}
	return audit.Event{
		Actor:       "public",
		ActorSource: "public",
		Action:      action,
		Method:      c.Request.Method,
		Path:        c.Request.URL.Path,
		Endpoint:    c.FullPath(),
		StatusCode:  status,
		Result:      result,
		RemoteAddr:  clientAddress(c),
		UserAgent:   c.Request.UserAgent(),
		RequestID:   requestID(c),
		DurationMS:  time.Since(start).Milliseconds(),
		Message:     message,
	}
}

func requestID(c *gin.Context) string {
	for _, header := range []string{"X-Request-ID", "X-Correlation-ID", "X-Amzn-Trace-Id"} {
		if value := strings.TrimSpace(c.GetHeader(header)); value != "" {
			return value
		}
	}
	return ""
}

func clientAddress(c *gin.Context) string {
	if value := strings.TrimSpace(c.GetHeader("X-Forwarded-For")); value != "" {
		if first, _, ok := strings.Cut(value, ","); ok {
			return strings.TrimSpace(first)
		}
		return value
	}
	if value := strings.TrimSpace(c.GetHeader("X-Real-IP")); value != "" {
		return value
	}
	return c.ClientIP()
}
