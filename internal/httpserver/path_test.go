package httpserver

import (
	"context"
	"log/slog"
	"net/http"
	"testing"
	"time"

	firebaseappchecksdk "firebase.google.com/go/v4/appcheck"
	"github.com/gin-gonic/gin"

	"github.com/example/turnstile-appcheck-gateway/internal/config"
	"github.com/example/turnstile-appcheck-gateway/internal/firebaseappcheck"
	"github.com/example/turnstile-appcheck-gateway/internal/handlers"
	"github.com/example/turnstile-appcheck-gateway/internal/health"
	"github.com/example/turnstile-appcheck-gateway/internal/turnstile"
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
		"POST /appcheck/api/v1/exchange",
		"GET /healthz",
		"GET /readyz",
	} {
		if !routes[required] {
			t.Fatalf("missing route: %s", required)
		}
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
