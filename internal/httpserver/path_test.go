package httpserver

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	firebaseappchecksdk "firebase.google.com/go/v4/appcheck"
	"github.com/gin-gonic/gin"

	"github.com/michibiki-io/turnstile-appcheck-gateway/internal/config"
	"github.com/michibiki-io/turnstile-appcheck-gateway/internal/firebaseappcheck"
	"github.com/michibiki-io/turnstile-appcheck-gateway/internal/handlers"
	"github.com/michibiki-io/turnstile-appcheck-gateway/internal/health"
	"github.com/michibiki-io/turnstile-appcheck-gateway/internal/turnstile"
)

func TestJoinSubpath(t *testing.T) {
	cases := []struct {
		subpath string
		suffix  string
		want    string
	}{
		{subpath: "/appcheck", suffix: "/api/v1/exchange", want: "/appcheck/api/v1/exchange"},
		{subpath: "appcheck/", suffix: "api/v1/verify", want: "/appcheck/api/v1/verify"},
		{subpath: "/", suffix: "/api/v1/verify", want: "/api/v1/verify"},
	}

	for _, tc := range cases {
		got := JoinSubpath(tc.subpath, tc.suffix)
		if got != tc.want {
			t.Fatalf("JoinSubpath(%q, %q) = %q; want %q", tc.subpath, tc.suffix, got, tc.want)
		}
	}
}

func TestNewRouterRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{
		AppCheckSubpath: "/appcheck",
		HealthPath:      "/healthz",
		ReadyPath:       "/readyz",
	}

	exchangeHandler := &handlers.ExchangeHandler{
		Logger:            slog.Default(),
		Timeout:           context.WithCancel,
		TurnstileVerifier: stubTurnstileVerifier{},
		Exchanger:         stubExchanger{},
	}
	verifyHandler := &handlers.VerifyHandler{
		Logger:        slog.Default(),
		Verifier:      stubVerifyVerifier{},
		HeaderName:    "X-Firebase-AppCheck",
		SuccessStatus: http.StatusNoContent,
		FailureStatus: http.StatusUnauthorized,
	}

	r, err := NewRouter(cfg, slog.Default(), exchangeHandler, verifyHandler, health.NewHandler())
	if err != nil {
		t.Fatalf("NewRouter() error = %v", err)
	}

	routes := map[string]bool{}
	for _, route := range r.Routes() {
		routes[route.Method+" "+route.Path] = true
	}

	for _, required := range []string{
		"OPTIONS /appcheck/api/v1/exchange",
		"POST /appcheck/api/v1/exchange",
		"GET /healthz",
		"GET /readyz",
	} {
		if !routes[required] {
			t.Fatalf("missing route: %s", required)
		}
	}
}

func TestNewRouterExchangePreflightRespondsWithCORSHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{
		AppCheckSubpath:        "/appcheck",
		HealthPath:             "/healthz",
		ReadyPath:              "/readyz",
		AllowedExchangeOrigins: []string{"https://example.com"},
	}

	exchangeHandler := &handlers.ExchangeHandler{
		Logger:            slog.Default(),
		Timeout:           context.WithCancel,
		TurnstileVerifier: stubTurnstileVerifier{},
		Exchanger:         stubExchanger{},
		OriginAllowed:     cfg.IsExchangeOriginAllowed,
	}
	verifyHandler := &handlers.VerifyHandler{
		Logger:        slog.Default(),
		Verifier:      stubVerifyVerifier{},
		HeaderName:    "X-Firebase-AppCheck",
		SuccessStatus: http.StatusNoContent,
		FailureStatus: http.StatusUnauthorized,
	}

	r, err := NewRouter(cfg, slog.Default(), exchangeHandler, verifyHandler, health.NewHandler())
	if err != nil {
		t.Fatalf("NewRouter() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodOptions, "/appcheck/api/v1/exchange", nil)
	req.Header.Set("Origin", "https://example.com")
	req.Header.Set("Access-Control-Request-Method", http.MethodPost)
	req.Header.Set("Access-Control-Request-Headers", "content-type")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "https://example.com" {
		t.Fatalf("Access-Control-Allow-Origin = %q; want %q", got, "https://example.com")
	}
	if got := w.Header().Get("Access-Control-Allow-Methods"); got != "POST, OPTIONS" {
		t.Fatalf("Access-Control-Allow-Methods = %q; want %q", got, "POST, OPTIONS")
	}
	if got := w.Header().Get("Access-Control-Allow-Headers"); got != "content-type" {
		t.Fatalf("Access-Control-Allow-Headers = %q; want %q", got, "content-type")
	}
}

func TestNewRouterExchangePostRespondsWithCORSHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{
		AppCheckSubpath:        "/appcheck",
		HealthPath:             "/healthz",
		ReadyPath:              "/readyz",
		AllowedExchangeOrigins: []string{"https://example.com"},
	}

	exchangeHandler := &handlers.ExchangeHandler{
		Logger:            slog.Default(),
		Timeout:           context.WithCancel,
		TurnstileVerifier: stubTurnstileVerifier{},
		Exchanger:         stubExchanger{},
		OriginAllowed:     cfg.IsExchangeOriginAllowed,
	}
	verifyHandler := &handlers.VerifyHandler{
		Logger:        slog.Default(),
		Verifier:      stubVerifyVerifier{},
		HeaderName:    "X-Firebase-AppCheck",
		SuccessStatus: http.StatusNoContent,
		FailureStatus: http.StatusUnauthorized,
	}

	r, err := NewRouter(cfg, slog.Default(), exchangeHandler, verifyHandler, health.NewHandler())
	if err != nil {
		t.Fatalf("NewRouter() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/appcheck/api/v1/exchange", bytes.NewBufferString(`{"turnstileToken":"token"}`))
	req.Header.Set("Origin", "https://example.com")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "https://example.com" {
		t.Fatalf("Access-Control-Allow-Origin = %q; want %q", got, "https://example.com")
	}
}

func TestNewRouterVerifyPreflightRespondsWithCORSHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{
		AppCheckSubpath:        "/appcheck",
		HealthPath:             "/healthz",
		ReadyPath:              "/readyz",
		AllowedExchangeOrigins: []string{"https://example.com"},
		VerifyHeaderName:       "X-Firebase-AppCheck",
	}

	exchangeHandler := &handlers.ExchangeHandler{
		Logger:            slog.Default(),
		Timeout:           context.WithCancel,
		TurnstileVerifier: stubTurnstileVerifier{},
		Exchanger:         stubExchanger{},
		OriginAllowed:     cfg.IsExchangeOriginAllowed,
	}
	verifyHandler := &handlers.VerifyHandler{
		Logger:        slog.Default(),
		Verifier:      stubVerifyVerifier{},
		HeaderName:    "X-Firebase-AppCheck",
		SuccessStatus: http.StatusNoContent,
		FailureStatus: http.StatusUnauthorized,
	}

	r, err := NewRouter(cfg, slog.Default(), exchangeHandler, verifyHandler, health.NewHandler())
	if err != nil {
		t.Fatalf("NewRouter() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodOptions, "/appcheck/api/v1/verify", nil)
	req.Header.Set("Origin", "https://example.com")
	req.Header.Set("Access-Control-Request-Method", http.MethodPost)
	req.Header.Set("Access-Control-Request-Headers", "content-type,x-firebase-appcheck")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "https://example.com" {
		t.Fatalf("Access-Control-Allow-Origin = %q; want %q", got, "https://example.com")
	}
	if got := w.Header().Get("Access-Control-Allow-Methods"); got != "GET, HEAD, POST, PUT, PATCH, DELETE, OPTIONS" {
		t.Fatalf("Access-Control-Allow-Methods = %q", got)
	}
	if got := w.Header().Get("Access-Control-Allow-Headers"); got != "content-type,x-firebase-appcheck" {
		t.Fatalf("Access-Control-Allow-Headers = %q; want %q", got, "content-type,x-firebase-appcheck")
	}
}

func TestNewRouterVerifyForwardAuthPreflightRespondsWithCORSHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{
		AppCheckSubpath:        "/appcheck",
		HealthPath:             "/healthz",
		ReadyPath:              "/readyz",
		AllowedExchangeOrigins: []string{"https://example.com"},
		VerifyHeaderName:       "X-Firebase-AppCheck",
	}

	exchangeHandler := &handlers.ExchangeHandler{
		Logger:            slog.Default(),
		Timeout:           context.WithCancel,
		TurnstileVerifier: stubTurnstileVerifier{},
		Exchanger:         stubExchanger{},
		OriginAllowed:     cfg.IsExchangeOriginAllowed,
	}
	verifyHandler := &handlers.VerifyHandler{
		Logger:        slog.Default(),
		Verifier:      stubVerifyVerifier{},
		HeaderName:    "X-Firebase-AppCheck",
		SuccessStatus: http.StatusNoContent,
		FailureStatus: http.StatusUnauthorized,
	}

	r, err := NewRouter(cfg, slog.Default(), exchangeHandler, verifyHandler, health.NewHandler())
	if err != nil {
		t.Fatalf("NewRouter() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/appcheck/api/v1/verify", nil)
	req.Header.Set("Origin", "https://example.com")
	req.Header.Set("Access-Control-Request-Method", http.MethodPost)
	req.Header.Set("Access-Control-Request-Headers", "content-type,x-firebase-appcheck")
	req.Header.Set("X-Forwarded-Method", http.MethodOptions)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "https://example.com" {
		t.Fatalf("Access-Control-Allow-Origin = %q; want %q", got, "https://example.com")
	}
}

func TestNewRouterVerifyGetWithoutTokenStillRequiresAppCheck(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{
		AppCheckSubpath:        "/appcheck",
		HealthPath:             "/healthz",
		ReadyPath:              "/readyz",
		AllowedExchangeOrigins: []string{"https://example.com"},
		VerifyHeaderName:       "X-Firebase-AppCheck",
	}

	exchangeHandler := &handlers.ExchangeHandler{
		Logger:            slog.Default(),
		Timeout:           context.WithCancel,
		TurnstileVerifier: stubTurnstileVerifier{},
		Exchanger:         stubExchanger{},
		OriginAllowed:     cfg.IsExchangeOriginAllowed,
	}
	verifyHandler := &handlers.VerifyHandler{
		Logger:        slog.Default(),
		Verifier:      stubVerifyVerifier{},
		HeaderName:    "X-Firebase-AppCheck",
		SuccessStatus: http.StatusNoContent,
		FailureStatus: http.StatusUnauthorized,
	}

	r, err := NewRouter(cfg, slog.Default(), exchangeHandler, verifyHandler, health.NewHandler())
	if err != nil {
		t.Fatalf("NewRouter() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/appcheck/api/v1/verify", nil)
	req.Header.Set("Origin", "https://example.com")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "https://example.com" {
		t.Fatalf("Access-Control-Allow-Origin = %q; want %q", got, "https://example.com")
	}
}

type stubTurnstileVerifier struct{}

func (stubTurnstileVerifier) Verify(_ context.Context, _ turnstile.VerifyRequest) (*turnstile.VerifyResponse, error) {
	return &turnstile.VerifyResponse{Success: true}, nil
}

type stubExchanger struct{}

func (stubExchanger) Exchange(_ context.Context, _ bool) (*firebaseappcheck.ExchangeResult, error) {
	return &firebaseappcheck.ExchangeResult{Token: "token", ExpireTimeMillis: time.Now().Add(time.Hour).UnixMilli()}, nil
}

type stubVerifyVerifier struct{}

func (stubVerifyVerifier) VerifyToken(_ string) (*firebaseappchecksdk.DecodedAppCheckToken, error) {
	return &firebaseappchecksdk.DecodedAppCheckToken{AppID: "1:test:web:abc"}, nil
}
