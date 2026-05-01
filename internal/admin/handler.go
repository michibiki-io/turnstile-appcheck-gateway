package admin

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/michibiki-io/turnstile-appcheck-gateway/internal/adminui"
	"github.com/michibiki-io/turnstile-appcheck-gateway/internal/audit"
	"github.com/michibiki-io/turnstile-appcheck-gateway/internal/config"
)

const identityKey = "adminIdentity"

type auditReader interface {
	audit.Recorder
	List(context.Context, audit.Filter) (audit.Page, error)
	Get(context.Context, string) (audit.Event, bool, error)
	Summary(context.Context, audit.Filter) (audit.Summary, error)
	Metrics(context.Context, audit.MetricsFilter) (audit.Metrics, error)
	Reset(context.Context, audit.Event) error
}

type Handler struct {
	cfg       *config.Config
	logger    *slog.Logger
	audit     audit.Recorder
	auditRead auditReader
}

func Register(root *gin.RouterGroup, cfg *config.Config, logger *slog.Logger, recorder audit.Recorder) {
	if cfg == nil || !cfg.Admin.Dashboard.Enabled {
		return
	}
	if logger == nil {
		logger = slog.Default()
	}
	if recorder == nil {
		recorder = audit.NoopRecorder{}
	}
	h := &Handler{cfg: cfg, logger: logger, audit: recorder}
	if reader, ok := recorder.(auditReader); ok {
		h.auditRead = reader
	}

	adminAPI := root.Group("/_admin/api/v1")
	adminAPI.Use(h.required())
	{
		adminAPI.GET("/me", h.me)
		adminAPI.GET("/request-metrics", h.requestMetrics)
		adminAPI.GET("/audit-options", h.auditOptions)
		adminAPI.GET("/audit-events", h.auditEvents)
		adminAPI.GET("/audit-events/:id", h.auditEvent)
		adminAPI.POST("/audit-events/reset", h.auditReset)
	}

	adminui.Register(root, cfg.Admin.Dashboard.BasePath, h.required(), h.dashboardAccess())
}

func (h *Handler) dashboardAccess() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		if !h.cfg.Audit.Enabled {
			return
		}
		name := c.Request.URL.Path[strings.LastIndex(c.Request.URL.Path, "/")+1:]
		if strings.Contains(name, ".") || name == "config.js" {
			return
		}
		identity := currentIdentity(c)
		h.record(c.Request.Context(), audit.Event{
			Actor:       identity.User,
			ActorSource: "admin:" + identity.AuthMode,
			Action:      "admin.dashboard.view",
			Method:      c.Request.Method,
			Path:        c.Request.URL.Path,
			Endpoint:    c.FullPath(),
			StatusCode:  c.Writer.Status(),
			Result:      resultFromStatus(c.Writer.Status()),
			RemoteAddr:  clientAddress(c),
			UserAgent:   c.Request.UserAgent(),
			RequestID:   requestID(c),
			DurationMS:  time.Since(start).Milliseconds(),
			Message:     "Admin dashboard accessed",
		})
	}
}

func (h *Handler) record(ctx context.Context, event audit.Event) {
	if h.audit == nil || !h.cfg.Audit.Enabled {
		return
	}
	if err := h.audit.Record(ctx, event); err != nil {
		h.logger.Warn("failed to record audit event", "error", err.Error())
	}
}

func resultFromStatus(status int) string {
	if status >= http.StatusBadRequest {
		return audit.ResultFailure
	}
	return audit.ResultSuccess
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
