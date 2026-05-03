package e2e

import (
	"context"
	"errors"
	"time"

	firebaseappchecksdk "firebase.google.com/go/v4/appcheck"

	"github.com/michibiki-io/turnstile-appcheck-gateway/internal/config"
	"github.com/michibiki-io/turnstile-appcheck-gateway/internal/firebaseappcheck"
	"github.com/michibiki-io/turnstile-appcheck-gateway/internal/turnstile"
)

var errMockAppCheckTokenInvalid = errors.New("mock app check token is invalid")

type MockTurnstileVerifier struct {
	passToken string
	failToken string
}

func NewMockTurnstileVerifier(cfg config.E2EConfig) *MockTurnstileVerifier {
	return &MockTurnstileVerifier{
		passToken: cfg.TurnstilePassToken,
		failToken: cfg.TurnstileFailToken,
	}
}

func (v *MockTurnstileVerifier) Verify(_ context.Context, req turnstile.VerifyRequest) (*turnstile.VerifyResponse, error) {
	if req.Token == v.passToken {
		return &turnstile.VerifyResponse{Success: true}, nil
	}
	errorCodes := []string{"invalid-input-response"}
	if req.Token == v.failToken {
		errorCodes = []string{"timeout-or-duplicate"}
	}
	return &turnstile.VerifyResponse{
		Success:    false,
		ErrorCodes: errorCodes,
	}, nil
}

type MockAppCheckExchanger struct {
	token string
	ttl   time.Duration
	now   func() time.Time
}

func NewMockAppCheckExchanger(cfg config.E2EConfig, ttl time.Duration) *MockAppCheckExchanger {
	if ttl <= 0 {
		ttl = 30 * time.Minute
	}
	return &MockAppCheckExchanger{
		token: cfg.AppCheckToken,
		ttl:   ttl,
		now:   time.Now,
	}
}

func (e *MockAppCheckExchanger) Exchange(_ context.Context, _ bool) (*firebaseappcheck.ExchangeResult, error) {
	expireAt := e.now().Add(e.ttl).UnixMilli()
	return &firebaseappcheck.ExchangeResult{
		Token:            e.token,
		ExpireTimeMillis: expireAt,
		TTLMillis:        e.ttl.Milliseconds(),
	}, nil
}

type MockVerifyTokenVerifier struct {
	token string
	appID string
}

func NewMockVerifyTokenVerifier(cfg config.E2EConfig) *MockVerifyTokenVerifier {
	return &MockVerifyTokenVerifier{
		token: cfg.AppCheckToken,
		appID: cfg.AppCheckAppID,
	}
}

func (v *MockVerifyTokenVerifier) VerifyToken(token string) (*firebaseappchecksdk.DecodedAppCheckToken, error) {
	if token != v.token {
		return nil, errMockAppCheckTokenInvalid
	}
	return &firebaseappchecksdk.DecodedAppCheckToken{AppID: v.appID}, nil
}
