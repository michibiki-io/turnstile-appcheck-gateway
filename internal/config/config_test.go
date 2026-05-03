package config

import (
	"encoding/base64"
	"testing"
	"time"
)

func minimalEnv() map[string]string {
	return map[string]string{
		"TURNSTILE_SECRET_KEY":        "secret",
		"FIREBASE_PROJECT_ID":         "demo-project",
		"FIREBASE_APP_ID":             "1:123456:web:abcdef",
		"GOOGLE_SERVICE_ACCOUNT_JSON": `{"type":"service_account","client_email":"svc@example.iam.gserviceaccount.com","private_key":"-----BEGIN PRIVATE KEY-----\\nabc\\n-----END PRIVATE KEY-----\\n"}`,
	}
}

func TestLoadFromMapConstructsDefaults(t *testing.T) {
	cfg, err := loadFromMap(minimalEnv())
	if err != nil {
		t.Fatalf("loadFromMap() error = %v", err)
	}
	if cfg.AppCheckSubpath != "/appcheck" {
		t.Fatalf("AppCheckSubpath = %q", cfg.AppCheckSubpath)
	}
	if cfg.HTTPAddr != ":8080" {
		t.Fatalf("HTTPAddr = %q", cfg.HTTPAddr)
	}
	if cfg.FirebaseAppResource != "projects/demo-project/apps/1:123456:web:abcdef" {
		t.Fatalf("FirebaseAppResource = %q", cfg.FirebaseAppResource)
	}
	if cfg.VerifyHeaderName != "X-Firebase-AppCheck" {
		t.Fatalf("VerifyHeaderName = %q", cfg.VerifyHeaderName)
	}
	if cfg.AppCheckTokenTTL != 30*time.Minute {
		t.Fatalf("AppCheckTokenTTL = %s", cfg.AppCheckTokenTTL)
	}
	if !cfg.Admin.Dashboard.Enabled || cfg.Admin.Dashboard.BasePath != "/admin" {
		t.Fatalf("Admin dashboard defaults = %#v", cfg.Admin.Dashboard)
	}
	if cfg.Admin.Auth.Mode != "header" || len(cfg.Admin.Auth.AllowedGroups) != 1 || cfg.Admin.Auth.AllowedGroups[0] != "gateway-admins" {
		t.Fatalf("Admin auth defaults = %#v", cfg.Admin.Auth)
	}
	if !cfg.Audit.Enabled || cfg.Audit.SQLitePath == "" || cfg.Audit.RetentionDays != 90 {
		t.Fatalf("Audit defaults = %#v", cfg.Audit)
	}
}

func TestLoadFromMapUsesAppResourceOverride(t *testing.T) {
	env := minimalEnv()
	env["FIREBASE_APP_RESOURCE"] = "projects/123456/apps/1:custom"
	delete(env, "FIREBASE_APP_ID")
	cfg, err := loadFromMap(env)
	if err != nil {
		t.Fatalf("loadFromMap() error = %v", err)
	}
	if cfg.FirebaseAppResource != "projects/123456/apps/1:custom" {
		t.Fatalf("FirebaseAppResource = %q", cfg.FirebaseAppResource)
	}
	if cfg.FirebaseAppID != "1:custom" {
		t.Fatalf("FirebaseAppID = %q", cfg.FirebaseAppID)
	}
}

func TestLoadFromMapBase64Precedence(t *testing.T) {
	env := minimalEnv()
	env["GOOGLE_SERVICE_ACCOUNT_JSON"] = "invalid"
	env["GOOGLE_SERVICE_ACCOUNT_JSON_BASE64"] = base64.StdEncoding.EncodeToString([]byte(`{"client_email":"ok@example.com","private_key":"k"}`))

	cfg, err := loadFromMap(env)
	if err != nil {
		t.Fatalf("loadFromMap() error = %v", err)
	}
	if string(cfg.ServiceAccountJSON) != `{"client_email":"ok@example.com","private_key":"k"}` {
		t.Fatalf("ServiceAccountJSON = %q", string(cfg.ServiceAccountJSON))
	}
}

func TestLoadFromMapInvalidOrigin(t *testing.T) {
	env := minimalEnv()
	env["ALLOWED_EXCHANGE_ORIGINS"] = "https://example.com/path"
	_, err := loadFromMap(env)
	if err == nil {
		t.Fatal("expected error for invalid origin with path")
	}
}

func TestLoadFromMapInvalidVerifyStatusSemantics(t *testing.T) {
	env := minimalEnv()
	env["VERIFY_SUCCESS_STATUS"] = "401"
	if _, err := loadFromMap(env); err == nil {
		t.Fatal("expected error for non-2xx VERIFY_SUCCESS_STATUS")
	}

	env = minimalEnv()
	env["VERIFY_FAILURE_STATUS"] = "204"
	if _, err := loadFromMap(env); err == nil {
		t.Fatal("expected error for 2xx VERIFY_FAILURE_STATUS")
	}
}

func TestLoadFromMapAppCheckTokenTTLOverride(t *testing.T) {
	env := minimalEnv()
	env["APPCHECK_TOKEN_TTL"] = "2h"

	cfg, err := loadFromMap(env)
	if err != nil {
		t.Fatalf("loadFromMap() error = %v", err)
	}
	if cfg.AppCheckTokenTTL != 2*time.Hour {
		t.Fatalf("AppCheckTokenTTL = %s", cfg.AppCheckTokenTTL)
	}
}

func TestLoadFromMapInvalidAppCheckTokenTTL(t *testing.T) {
	env := minimalEnv()
	env["APPCHECK_TOKEN_TTL"] = "abc"
	if _, err := loadFromMap(env); err == nil {
		t.Fatal("expected error for invalid APPCHECK_TOKEN_TTL format")
	}

	env = minimalEnv()
	env["APPCHECK_TOKEN_TTL"] = "10m"
	if _, err := loadFromMap(env); err == nil {
		t.Fatal("expected error for APPCHECK_TOKEN_TTL < 30m")
	}

	env = minimalEnv()
	env["APPCHECK_TOKEN_TTL"] = "169h"
	if _, err := loadFromMap(env); err == nil {
		t.Fatal("expected error for APPCHECK_TOKEN_TTL > 7d")
	}
}

func TestLoadFromMapAdminAuditAndRateLimitEnv(t *testing.T) {
	env := minimalEnv()
	env["ADMIN_DASHBOARD_ENABLED"] = "false"
	env["ADMIN_BASE_PATH"] = "ops"
	env["ADMIN_AUDIT_TIMESTAMP_FORMAT"] = "2006/01/02 15:04 MST"
	env["ADMIN_AUDIT_TIMESTAMP_TIMEZONE"] = "UTC"
	env["ADMIN_AUTH_MODE"] = "none"
	env["ADMIN_ALLOWED_USERS"] = "admin@example.com, ops@example.com"
	env["ADMIN_ALLOWED_GROUPS"] = "admins, operators"
	env["AUDIT_ENABLED"] = "false"
	env["AUDIT_SQLITE_PATH"] = "/tmp/audit.db"
	env["AUDIT_RETENTION_DAYS"] = "7"
	env["RATE_LIMIT_ENABLED"] = "false"
	env["RATE_LIMIT_REQUESTS"] = "5"
	env["RATE_LIMIT_WINDOW"] = "10s"

	cfg, err := loadFromMap(env)
	if err != nil {
		t.Fatalf("loadFromMap() error = %v", err)
	}
	if cfg.Admin.Dashboard.Enabled || cfg.Admin.Dashboard.BasePath != "/ops" {
		t.Fatalf("Admin dashboard env = %#v", cfg.Admin.Dashboard)
	}
	if cfg.Admin.Auth.Mode != "none" || len(cfg.Admin.Auth.AllowedUsers) != 2 || len(cfg.Admin.Auth.AllowedGroups) != 2 {
		t.Fatalf("Admin auth env = %#v", cfg.Admin.Auth)
	}
	if cfg.Audit.Enabled || cfg.Audit.SQLitePath != "/tmp/audit.db" || cfg.Audit.RetentionDays != 7 {
		t.Fatalf("Audit env = %#v", cfg.Audit)
	}
	if cfg.RateLimit.Enabled || cfg.RateLimit.RequestsPerWindow != 5 || cfg.RateLimit.Window != 10*time.Second {
		t.Fatalf("RateLimit env = %#v", cfg.RateLimit)
	}
}

func TestLoadFromMapMissingRequired(t *testing.T) {
	_, err := loadFromMap(map[string]string{})
	if err == nil {
		t.Fatal("expected error for missing required env vars")
	}
}

func TestLoadFromMapMockModeSkipsRealUpstreamRequirements(t *testing.T) {
	cfg, err := loadFromMap(map[string]string{
		"E2E_UPSTREAM_MODE": "mock",
	})
	if err != nil {
		t.Fatalf("loadFromMap() error = %v", err)
	}
	if !cfg.IsMockUpstreamMode() {
		t.Fatal("expected mock upstream mode to be enabled")
	}
	if cfg.E2E.TurnstilePassToken != "e2e-turnstile-pass" {
		t.Fatalf("TurnstilePassToken = %q", cfg.E2E.TurnstilePassToken)
	}
	if cfg.E2E.AppCheckToken != "e2e-appcheck-valid-token" {
		t.Fatalf("AppCheckToken = %q", cfg.E2E.AppCheckToken)
	}
}

func TestLoadFromMapRejectsInvalidMockMode(t *testing.T) {
	_, err := loadFromMap(map[string]string{
		"E2E_UPSTREAM_MODE": "real",
	})
	if err == nil {
		t.Fatal("expected error for invalid E2E_UPSTREAM_MODE")
	}
}

func TestIsExchangeOriginAllowed(t *testing.T) {
	env := minimalEnv()
	env["ALLOWED_EXCHANGE_ORIGINS"] = "https://example.com,https://www.example.com"
	cfg, err := loadFromMap(env)
	if err != nil {
		t.Fatalf("loadFromMap() error = %v", err)
	}
	if !cfg.IsExchangeOriginAllowed("https://example.com") {
		t.Fatal("expected exact origin to be allowed")
	}
	if cfg.IsExchangeOriginAllowed("https://evil.com") {
		t.Fatal("expected unknown origin to be denied")
	}
}
