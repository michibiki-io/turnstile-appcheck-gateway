package httpserver

import (
	"fmt"
	"log/slog"

	"github.com/gin-gonic/gin"

	"github.com/michibiki-io/turnstile-appcheck-gateway/internal/admin"
	"github.com/michibiki-io/turnstile-appcheck-gateway/internal/audit"
	"github.com/michibiki-io/turnstile-appcheck-gateway/internal/config"
	"github.com/michibiki-io/turnstile-appcheck-gateway/internal/handlers"
	"github.com/michibiki-io/turnstile-appcheck-gateway/internal/health"
	"github.com/michibiki-io/turnstile-appcheck-gateway/internal/middleware"
)

// NewRouter configures Gin routes and middleware.
func NewRouter(cfg *config.Config, logger *slog.Logger, exchangeHandler *handlers.ExchangeHandler, verifyHandler *handlers.VerifyHandler, healthHandler *health.Handler, recorders ...audit.Recorder) (*gin.Engine, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is required")
	}
	if logger == nil {
		return nil, fmt.Errorf("logger is required")
	}
	if exchangeHandler == nil || verifyHandler == nil || healthHandler == nil {
		return nil, fmt.Errorf("handlers are required")
	}

	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(middleware.Recovery(logger), middleware.RequestLogger(logger))

	if cfg.TrustProxyHeaders {
		if err := r.SetTrustedProxies([]string{"0.0.0.0/0", "::/0"}); err != nil {
			return nil, fmt.Errorf("set trusted proxies: %w", err)
		}
	} else {
		if err := r.SetTrustedProxies(nil); err != nil {
			return nil, fmt.Errorf("disable trusted proxies: %w", err)
		}
	}

	r.GET(cfg.HealthPath, gin.WrapF(healthHandler.Healthz))
	r.GET(cfg.ReadyPath, gin.WrapF(healthHandler.Readyz))

	recorder := audit.Recorder(audit.NoopRecorder{})
	if len(recorders) > 0 && recorders[0] != nil {
		recorder = recorders[0]
	}

	api := r.Group(cfg.AppCheckSubpath)
	public := api.Group("", middleware.NewRateLimiter(cfg.RateLimit, logger, recorder))
	public.POST("/api/v1/exchange", exchangeHandler.Handle)
	public.Any("/api/v1/verify", verifyHandler.Handle)
	admin.Register(api, cfg, logger, recorder)

	return r, nil
}
