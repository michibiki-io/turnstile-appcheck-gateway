package admin

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/michibiki-io/turnstile-appcheck-gateway/internal/audit"
	"github.com/michibiki-io/turnstile-appcheck-gateway/internal/version"
)

func (h *Handler) me(c *gin.Context) {
	identity := currentIdentity(c)
	c.JSON(http.StatusOK, gin.H{
		"mode":                 h.cfg.Admin.Auth.Mode,
		"authDisabled":         identity.AuthDisabled,
		"user":                 identity.User,
		"email":                identity.Email,
		"groups":               identity.Groups,
		"version":              version.Value(),
		"commit":               version.Commit(),
		"shortCommit":          version.ShortCommit(),
		"commitURL":            commitURL(version.Commit()),
		"auditTimestampFormat": h.cfg.Admin.Dashboard.TimestampFormat,
	})
}

func (h *Handler) requestMetrics(c *gin.Context) {
	if h.auditRead == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "audit storage is not available"})
		return
	}
	filter := audit.MetricsFilter{
		From:        queryTime(c, "from"),
		To:          queryTime(c, "to"),
		Bucket:      queryDuration(c, "bucket"),
		Endpoint:    c.Query("endpoint"),
		Method:      c.Query("method"),
		Result:      c.Query("result"),
		StatusClass: c.Query("status_class"),
	}
	metrics, err := h.auditRead.Metrics(c.Request.Context(), filter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load request metrics"})
		return
	}
	h.recordAdminAPI(c, "admin.metrics.view", "Admin viewed request metrics")
	c.JSON(http.StatusOK, metrics)
}

func (h *Handler) auditOptions(c *gin.Context) {
	prefix := strings.TrimRight(h.cfg.AppCheckSubpath, "/")
	c.JSON(http.StatusOK, gin.H{
		"actions": audit.KnownActions(),
		"endpoints": []string{
			prefix + "/api/v1/exchange",
			prefix + "/api/v1/verify",
			prefix + "/_admin/api/v1/me",
			prefix + "/_admin/api/v1/request-metrics",
			prefix + "/_admin/api/v1/audit-events",
		},
		"results": []string{audit.ResultSuccess, audit.ResultFailure, audit.ResultDenied},
	})
}

func (h *Handler) auditEvents(c *gin.Context) {
	if h.auditRead == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "audit storage is not available"})
		return
	}
	filter := audit.Filter{
		From:         queryTime(c, "from"),
		To:           queryTime(c, "to"),
		Actor:        c.Query("actor"),
		Action:       c.Query("action"),
		Endpoint:     c.Query("endpoint"),
		Path:         c.Query("path"),
		Method:       c.Query("method"),
		Result:       c.Query("result"),
		StatusCode:   queryInt(c, "status_code", 0),
		StatusClass:  c.Query("status_class"),
		RequestID:    c.Query("request_id"),
		Limit:        queryInt(c, "limit", 50),
		Cursor:       c.Query("cursor"),
		Offset:       queryInt(c, "offset", 0),
		IncludeTotal: true,
	}
	legacyOffsetMode := strings.TrimSpace(c.Query("cursor")) == "" && strings.TrimSpace(c.Query("offset")) != ""
	if strings.TrimSpace(filter.Cursor) != "" {
		if _, err := audit.DecodeCursor(filter.Cursor); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid audit cursor"})
			return
		}
	}
	page, err := h.auditRead.List(c.Request.Context(), filter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load audit events"})
		return
	}
	var nextCursor any
	if legacyOffsetMode {
		limit := filter.Limit
		if limit <= 0 {
			limit = 50
		}
		nextOffset := filter.Offset + limit
		if nextOffset < page.Total {
			nextCursor = nextOffset
		}
	} else if page.NextCursor != "" {
		nextCursor = page.NextCursor
	}
	h.recordAdminAPI(c, "audit.view", "Admin viewed audit logs")
	c.JSON(http.StatusOK, gin.H{
		"items":      h.auditEventResponses(page.Items),
		"total":      page.Total,
		"nextCursor": nextCursor,
		"hasNext":    page.HasNext,
	})
}

func (h *Handler) auditEvent(c *gin.Context) {
	if h.auditRead == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "audit storage is not available"})
		return
	}
	event, ok, err := h.auditRead.Get(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load audit event"})
		return
	}
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "audit event not found"})
		return
	}
	h.recordAdminAPI(c, "audit.detail.view", "Admin viewed audit log detail")
	c.JSON(http.StatusOK, h.auditEventResponse(event))
}

type auditEventResponse struct {
	audit.Event
	TimestampDisplay string `json:"timestampDisplay"`
}

func (h *Handler) auditEventResponses(events []audit.Event) []auditEventResponse {
	out := make([]auditEventResponse, 0, len(events))
	for _, event := range events {
		out = append(out, h.auditEventResponse(event))
	}
	return out
}

func (h *Handler) auditEventResponse(event audit.Event) auditEventResponse {
	return auditEventResponse{
		Event:            event,
		TimestampDisplay: h.formatAuditTimestamp(event.Timestamp),
	}
}

func (h *Handler) formatAuditTimestamp(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	location := time.UTC
	if configured := strings.TrimSpace(h.cfg.Admin.Dashboard.TimestampTimezone); configured != "" {
		if loaded, err := time.LoadLocation(configured); err == nil {
			location = loaded
		}
	}
	layout := strings.TrimSpace(h.cfg.Admin.Dashboard.TimestampFormat)
	if layout == "" {
		layout = time.RFC3339
	}
	return value.In(location).Format(layout)
}

func commitURL(commit string) string {
	commit = strings.TrimSpace(commit)
	if commit == "" || commit == "unknown" {
		return ""
	}
	return "https://github.com/michibiki-io/turnstile-appcheck-gateway/commit/" + commit
}

func (h *Handler) auditReset(c *gin.Context) {
	if h.auditRead == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "audit storage is not available"})
		return
	}
	var body struct {
		Confirmation string `json:"confirmation"`
		Reason       string `json:"reason"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid reset request"})
		return
	}
	if body.Confirmation != "RESET" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "confirmation must be RESET"})
		return
	}
	identity := currentIdentity(c)
	marker := audit.Event{
		Actor:       identity.User,
		ActorSource: "admin:" + identity.AuthMode,
		Action:      "audit.reset",
		Method:      c.Request.Method,
		Path:        c.Request.URL.Path,
		Endpoint:    c.FullPath(),
		StatusCode:  http.StatusOK,
		Result:      audit.ResultSuccess,
		RemoteAddr:  clientAddress(c),
		UserAgent:   c.Request.UserAgent(),
		RequestID:   requestID(c),
		Message:     "Admin reset audit log",
		Metadata: map[string]any{
			"reason": strings.TrimSpace(body.Reason),
		},
	}
	if err := h.auditRead.Reset(c.Request.Context(), marker); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to reset audit events"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (h *Handler) recordAdminAPI(c *gin.Context, action, message string) {
	identity := currentIdentity(c)
	h.record(c.Request.Context(), audit.Event{
		Actor:       identity.User,
		ActorSource: "admin:" + identity.AuthMode,
		Action:      action,
		Method:      c.Request.Method,
		Path:        c.Request.URL.Path,
		Endpoint:    c.FullPath(),
		StatusCode:  c.Writer.Status(),
		Result:      audit.ResultSuccess,
		RemoteAddr:  clientAddress(c),
		UserAgent:   c.Request.UserAgent(),
		RequestID:   requestID(c),
		Message:     message,
	})
}

func queryInt(c *gin.Context, key string, fallback int) int {
	value := strings.TrimSpace(c.Query(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func queryTime(c *gin.Context, key string) time.Time {
	value := strings.TrimSpace(c.Query(key))
	if value == "" {
		return time.Time{}
	}
	if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return parsed
	}
	if parsed, err := time.Parse("2006-01-02", value); err == nil {
		return parsed
	}
	return time.Time{}
}

func queryDuration(c *gin.Context, key string) time.Duration {
	value := strings.TrimSpace(c.Query(key))
	if value == "" {
		return 0
	}
	if parsed, err := time.ParseDuration(value); err == nil {
		return parsed
	}
	return 0
}
