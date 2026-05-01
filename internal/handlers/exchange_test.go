package handlers

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/michibiki-io/turnstile-appcheck-gateway/internal/firebaseappcheck"
	"github.com/michibiki-io/turnstile-appcheck-gateway/internal/turnstile"
)

func TestExchangeHandlerSuccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &ExchangeHandler{
		Logger: slog.Default(),
		Timeout: func(parent context.Context) (context.Context, context.CancelFunc) {
			return context.WithTimeout(parent, time.Second)
		},
		TurnstileVerifier: exchangeTurnstileStub{resp: &turnstile.VerifyResponse{Success: true}},
		Exchanger:         exchangeStub{result: &firebaseappcheck.ExchangeResult{Token: "appcheck-token", ExpireTimeMillis: 1000}},
		OriginAllowed:     func(origin string) bool { return true },
	}

	r := gin.New()
	r.POST("/exchange", h.Handle)

	req := httptest.NewRequest(http.MethodPost, "/exchange", bytes.NewBufferString(`{"turnstileToken":"token","limitedUse":true}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
}

func TestExchangeHandlerMalformedJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &ExchangeHandler{
		Logger:            slog.Default(),
		Timeout:           context.WithCancel,
		TurnstileVerifier: exchangeTurnstileStub{resp: &turnstile.VerifyResponse{Success: true}},
		Exchanger:         exchangeStub{result: &firebaseappcheck.ExchangeResult{Token: "appcheck-token", ExpireTimeMillis: 1000}},
	}

	r := gin.New()
	r.POST("/exchange", h.Handle)

	req := httptest.NewRequest(http.MethodPost, "/exchange", bytes.NewBufferString(`{"turnstileToken":`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
}

func TestExchangeHandlerMissingHeaderContentType(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &ExchangeHandler{
		Logger:            slog.Default(),
		Timeout:           context.WithCancel,
		TurnstileVerifier: exchangeTurnstileStub{resp: &turnstile.VerifyResponse{Success: true}},
		Exchanger:         exchangeStub{result: &firebaseappcheck.ExchangeResult{Token: "appcheck-token", ExpireTimeMillis: 1000}},
	}

	r := gin.New()
	r.POST("/exchange", h.Handle)

	req := httptest.NewRequest(http.MethodPost, "/exchange", bytes.NewBufferString(`{"turnstileToken":"token"}`))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
}

func TestExchangeHandlerTurnstileFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &ExchangeHandler{
		Logger:            slog.Default(),
		Timeout:           context.WithCancel,
		TurnstileVerifier: exchangeTurnstileStub{resp: &turnstile.VerifyResponse{Success: false, ErrorCodes: []string{"timeout-or-duplicate"}}},
		Exchanger:         exchangeStub{result: &firebaseappcheck.ExchangeResult{Token: "appcheck-token", ExpireTimeMillis: 1000}},
	}

	r := gin.New()
	r.POST("/exchange", h.Handle)

	req := httptest.NewRequest(http.MethodPost, "/exchange", bytes.NewBufferString(`{"turnstileToken":"token"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
}

func TestExchangeHandlerOriginDenied(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &ExchangeHandler{
		Logger:            slog.Default(),
		Timeout:           context.WithCancel,
		TurnstileVerifier: exchangeTurnstileStub{resp: &turnstile.VerifyResponse{Success: true}},
		Exchanger:         exchangeStub{result: &firebaseappcheck.ExchangeResult{Token: "appcheck-token", ExpireTimeMillis: 1000}},
		OriginAllowed:     func(origin string) bool { return false },
	}

	r := gin.New()
	r.POST("/exchange", h.Handle)

	req := httptest.NewRequest(http.MethodPost, "/exchange", bytes.NewBufferString(`{"turnstileToken":"token"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://evil.com")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
}

func TestExchangeHandlerExchangeFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &ExchangeHandler{
		Logger:            slog.Default(),
		Timeout:           context.WithCancel,
		TurnstileVerifier: exchangeTurnstileStub{resp: &turnstile.VerifyResponse{Success: true}},
		Exchanger:         exchangeStub{err: errors.New("upstream")},
	}

	r := gin.New()
	r.POST("/exchange", h.Handle)

	req := httptest.NewRequest(http.MethodPost, "/exchange", bytes.NewBufferString(`{"turnstileToken":"token"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
}

type exchangeTurnstileStub struct {
	resp *turnstile.VerifyResponse
	err  error
}

func (s exchangeTurnstileStub) Verify(_ context.Context, _ turnstile.VerifyRequest) (*turnstile.VerifyResponse, error) {
	return s.resp, s.err
}

type exchangeStub struct {
	result *firebaseappcheck.ExchangeResult
	err    error
}

func (s exchangeStub) Exchange(_ context.Context, _ bool) (*firebaseappcheck.ExchangeResult, error) {
	return s.result, s.err
}
