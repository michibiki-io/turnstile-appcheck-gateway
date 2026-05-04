package middleware

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/michibiki-io/turnstile-appcheck-gateway/internal/audit"
	"github.com/michibiki-io/turnstile-appcheck-gateway/internal/audit/storage/bunrepo"
	"github.com/michibiki-io/turnstile-appcheck-gateway/internal/config"
)

func TestRateLimiterDeniesAndAudits(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store, err := bunrepo.Open(context.Background(), bunrepo.Config{StorageType: "sqlite", SQLitePath: filepath.Join(t.TempDir(), "audit.db")})
	if err != nil {
		t.Fatalf("audit.Open() error = %v", err)
	}
	defer store.Close()

	router := gin.New()
	router.GET("/verify", NewRateLimiter(config.RateLimitConfig{
		Enabled:           true,
		RequestsPerWindow: 1,
		Window:            time.Minute,
	}, slog.Default(), audit.NewSyncRecorder(store, audit.NewPolicy(audit.PolicyConfig{}))), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodGet, "/verify", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("first status = %d", w.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/verify", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("second status = %d body = %s", w.Code, w.Body.String())
	}

	page, err := store.List(context.Background(), audit.Filter{Action: "rate_limit.denied", IncludeTotal: true})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if page.Total != 1 || page.Items[0].Result != audit.ResultDenied {
		t.Fatalf("rate limit audit missing: %#v", page)
	}
}
