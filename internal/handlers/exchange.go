package handlers

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	apperrors "github.com/example/turnstile-appcheck-gateway/internal/errors"
	"github.com/example/turnstile-appcheck-gateway/internal/firebaseappcheck"
	"github.com/example/turnstile-appcheck-gateway/internal/model"
	"github.com/example/turnstile-appcheck-gateway/internal/turnstile"
)

// ExchangeHandler implements POST /api/v1/exchange.
type ExchangeHandler struct {
	Logger            *slog.Logger
	Timeout           func(context.Context) (context.Context, context.CancelFunc)
	TurnstileVerifier turnstile.Verifier
	Exchanger         firebaseappcheck.Exchanger
	TrustProxyHeaders bool
	OriginAllowed     func(string) bool
}

func (h *ExchangeHandler) Handle(c *gin.Context) {
	if !hasJSONContentType(c.Request) {
		apperrors.WriteError(c, apperrors.New(http.StatusUnsupportedMediaType, "UNSUPPORTED_CONTENT_TYPE", "Content-Type must be application/json", nil))
		return
	}

	if h.OriginAllowed != nil {
		origin := c.GetHeader("Origin")
		if !h.OriginAllowed(origin) {
			apperrors.WriteError(c, apperrors.Forbidden("EXCHANGE_ORIGIN_NOT_ALLOWED", "Origin is not allowed", nil))
			return
		}
	}

	var req model.ExchangeRequest
	if err := decodeJSONStrict(c.Request.Body, &req); err != nil {
		apperrors.WriteError(c, apperrors.BadRequest("INVALID_JSON", "Request body must be valid JSON", err))
		return
	}
	if req.TurnstileToken == "" {
		apperrors.WriteError(c, apperrors.BadRequest("TURNSTILE_TOKEN_REQUIRED", "turnstileToken is required", nil))
		return
	}

	timeoutFn := h.Timeout
	if timeoutFn == nil {
		timeoutFn = context.WithCancel
	}
	ctx, cancel := timeoutFn(c.Request.Context())
	defer cancel()

	verifyResp, err := h.TurnstileVerifier.Verify(ctx, turnstile.VerifyRequest{
		Token:    req.TurnstileToken,
		RemoteIP: extractRemoteIP(c.Request, h.TrustProxyHeaders),
	})
	if err != nil {
		h.Logger.Warn("turnstile siteverify failed", "error", err.Error())
		apperrors.WriteError(c, apperrors.New(http.StatusBadGateway, "TURNSTILE_UPSTREAM_ERROR", "Failed to validate Turnstile token", err))
		return
	}
	if !verifyResp.Success {
		h.Logger.Info("turnstile validation rejected", "error_codes", verifyResp.ErrorCodes)
		apperrors.WriteError(c, apperrors.BadRequest("TURNSTILE_VALIDATION_FAILED", "Turnstile token is invalid or expired", nil))
		return
	}

	exchanged, err := h.Exchanger.Exchange(ctx, req.LimitedUse)
	if err != nil {
		h.Logger.Error("firebase app check exchange failed", "error", err.Error())
		apperrors.WriteError(c, apperrors.New(http.StatusBadGateway, "FIREBASE_EXCHANGE_FAILED", "Failed to exchange App Check token", err))
		return
	}

	c.JSON(http.StatusOK, model.ExchangeResponse{
		Token:            exchanged.Token,
		ExpireTimeMillis: exchanged.ExpireTimeMillis,
	})
}
