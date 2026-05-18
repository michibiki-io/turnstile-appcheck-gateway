package admin

import (
	"bytes"
	"context"
	"encoding/json"
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

func TestHeaderAuthAllowDenyAndMe(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := testConfig(t)
	store := testStore(t)
	router := gin.New()
	root := router.Group(cfg.AppCheckSubpath)
	Register(root, cfg, slog.Default(), store)

	req := httptest.NewRequest(http.MethodGet, "/appcheck/_admin/api/v1/me", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("missing identity status = %d body = %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/appcheck/_admin/api/v1/me", nil)
	req.Header.Set("X-Forwarded-User", "viewer@example.com")
	req.Header.Set("X-Forwarded-Groups", "viewers")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("unauthorized identity status = %d body = %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/appcheck/_admin/api/v1/me", nil)
	req.Header.Set("X-Forwarded-User", "admin@example.com")
	req.Header.Set("X-Forwarded-Groups", "gateway-admins")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("allowed status = %d body = %s", w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("me response json: %v", err)
	}
	if body["version"] == "" || body["commit"] == "" || body["shortCommit"] == "" {
		t.Fatalf("version fields missing: %#v", body)
	}
}

func TestReleaseTagURL(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{
			name:  "docker tag version",
			value: "1.2.3",
			want:  "https://github.com/michibiki-io/turnstile-appcheck-gateway/releases/tag/v1.2.3",
		},
		{
			name:  "git tag version",
			value: "v1.2.3",
			want:  "https://github.com/michibiki-io/turnstile-appcheck-gateway/releases/tag/v1.2.3",
		},
		{name: "dev", value: "dev", want: ""},
		{name: "non semver", value: "main", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := releaseTagURL(tt.value); got != tt.want {
				t.Fatalf("releaseTagURL(%q) = %q, want %q", tt.value, got, tt.want)
			}
		})
	}
}

func TestNoneAuthAndTimestampFormatting(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := testConfig(t)
	cfg.Admin.Auth.Mode = "none"
	cfg.Admin.Dashboard.TimestampFormat = "2006/01/02 15:04 MST"
	cfg.Admin.Dashboard.TimestampTimezone = "Asia/Tokyo"
	store := testStore(t)
	ts := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := store.Record(context.Background(), audit.Event{Timestamp: ts, Actor: "public", Action: "exchange.request", Result: audit.ResultSuccess}); err != nil {
		t.Fatalf("Record() error = %v", err)
	}

	router := gin.New()
	Register(router.Group(cfg.AppCheckSubpath), cfg, slog.Default(), store)

	req := httptest.NewRequest(http.MethodGet, "/appcheck/_admin/api/v1/audit-events?limit=1", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
	var body struct {
		Items []struct {
			TimestampDisplay string `json:"timestampDisplay"`
		} `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("audit response json: %v", err)
	}
	if len(body.Items) == 0 || body.Items[0].TimestampDisplay != "2026/01/02 12:04 JST" {
		t.Fatalf("timestampDisplay = %#v", body.Items)
	}
}

func TestAuditResetRequiresConfirmation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := testConfig(t)
	cfg.Admin.Auth.Mode = "none"
	store := testStore(t)
	if err := store.Record(context.Background(), audit.Event{Action: "exchange.request", Result: audit.ResultSuccess}); err != nil {
		t.Fatalf("Record() error = %v", err)
	}

	router := gin.New()
	Register(router.Group(cfg.AppCheckSubpath), cfg, slog.Default(), store)

	req := httptest.NewRequest(http.MethodPost, "/appcheck/_admin/api/v1/audit-events/reset", bytes.NewBufferString(`{"confirmation":"NO"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("bad confirmation status = %d body = %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/appcheck/_admin/api/v1/audit-events/reset", bytes.NewBufferString(`{"confirmation":"RESET","reason":"test"}`))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("reset status = %d body = %s", w.Code, w.Body.String())
	}
	page, err := store.List(context.Background(), audit.Filter{IncludeTotal: true})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if page.Total != 1 || page.Items[0].Action != "audit.reset" {
		t.Fatalf("reset marker missing: %#v", page)
	}
}

func testConfig(t *testing.T) *config.Config {
	t.Helper()
	return &config.Config{
		AppCheckSubpath: "/appcheck",
		Admin: config.AdminConfig{
			Dashboard: config.AdminDashboardConfig{
				Enabled:           true,
				BasePath:          "/admin",
				TimestampFormat:   "2006-01-02 15:04:05 MST",
				TimestampTimezone: "UTC",
			},
			Auth: config.AdminAuthConfig{
				Mode:          "header",
				UserHeader:    "X-Forwarded-User",
				EmailHeader:   "X-Forwarded-Email",
				GroupsHeader:  "X-Forwarded-Groups",
				AllowedGroups: []string{"gateway-admins"},
			},
		},
		Audit: config.AuditConfig{Enabled: true},
	}
}

func testStore(t *testing.T) *audit.SyncRecorder {
	t.Helper()
	store, err := bunrepo.Open(context.Background(), bunrepo.Config{StorageType: "sqlite", SQLitePath: filepath.Join(t.TempDir(), "audit.db")})
	if err != nil {
		t.Fatalf("audit.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return audit.NewSyncRecorder(store, audit.NewPolicy(audit.PolicyConfig{}))
}
