package handlers

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/michibiki-io/turnstile-appcheck-gateway/internal/audit"
	apperrors "github.com/michibiki-io/turnstile-appcheck-gateway/internal/errors"
	"github.com/michibiki-io/turnstile-appcheck-gateway/internal/firebaseappcheck"
	"github.com/michibiki-io/turnstile-appcheck-gateway/internal/model"
	"github.com/michibiki-io/turnstile-appcheck-gateway/internal/turnstile"
)

// ExchangeHandler implements POST /api/v1/exchange.
type ExchangeHandler struct {
	Logger            *slog.Logger
	Timeout           func(context.Context) (context.Context, context.CancelFunc)
	TurnstileVerifier turnstile.Verifier
	Exchanger         firebaseappcheck.Exchanger
	TrustProxyHeaders bool
	OriginAllowed     func(string) bool
	Audit             audit.Recorder
}

func (h *ExchangeHandler) Handle(c *gin.Context) {
	start := time.Now()
	limitedUse := false
	defer func() {
		event := requestAuditEvent(c, start, "exchange.request", "Exchange request completed")
		event.Metadata = map[string]any{"limitedUse": limitedUse}
		recordAudit(c.Request.Context(), h.Logger, h.Audit, event)
	}()

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
	limitedUse = req.LimitedUse
	if len(req.TurnstileToken) > 4096 {
		apperrors.WriteError(c, apperrors.BadRequest("TURNSTILE_TOKEN_TOO_LARGE", "turnstileToken is too large", nil))
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
		h.recordExchangeStep(c, start, "turnstile.verify", http.StatusBadGateway, audit.ResultFailure, "turnstile_upstream_error", "Turnstile verification failed", map[string]any{"service": "turnstile", "limitedUse": req.LimitedUse})
		apperrors.WriteError(c, apperrors.New(http.StatusBadGateway, "TURNSTILE_UPSTREAM_ERROR", "Failed to validate Turnstile token", err))
		return
	}
	if !verifyResp.Success {
		h.Logger.Info("turnstile validation rejected", "error_codes", verifyResp.ErrorCodes)
		h.recordExchangeStep(c, start, "turnstile.verify", http.StatusBadRequest, audit.ResultFailure, "turnstile_validation_failed", "Turnstile token rejected", map[string]any{"service": "turnstile", "limitedUse": req.LimitedUse, "errorCodes": verifyResp.ErrorCodes})
		apperrors.WriteError(c, apperrors.BadRequest("TURNSTILE_VALIDATION_FAILED", "Turnstile token is invalid or expired", nil))
		return
	}
	h.recordExchangeStep(c, start, "turnstile.verify", http.StatusOK, audit.ResultSuccess, "", "Turnstile token verified", map[string]any{"service": "turnstile", "limitedUse": req.LimitedUse})

	exchanged, err := h.Exchanger.Exchange(ctx, req.LimitedUse)
	if err != nil {
		h.Logger.Error("firebase app check exchange failed", "error", err.Error())
		h.recordExchangeStep(c, start, "appcheck.exchange", http.StatusBadGateway, audit.ResultFailure, "firebase_exchange_failed", "Firebase App Check custom token exchange failed", map[string]any{"service": "firebase_appcheck", "limitedUse": req.LimitedUse})
		apperrors.WriteError(c, apperrors.New(http.StatusBadGateway, "FIREBASE_EXCHANGE_FAILED", "Failed to exchange App Check token", err))
		return
	}
	h.recordExchangeStep(c, start, "appcheck.exchange", http.StatusOK, audit.ResultSuccess, "", "Firebase App Check token exchange succeeded", map[string]any{"service": "firebase_appcheck", "limitedUse": req.LimitedUse})

	c.JSON(http.StatusOK, model.ExchangeResponse{
		Token:            exchanged.Token,
		ExpireTimeMillis: exchanged.ExpireTimeMillis,
	})
}

func (h *ExchangeHandler) recordExchangeStep(c *gin.Context, start time.Time, action string, status int, result string, errorCode string, message string, metadata map[string]any) {
	event := requestAuditEvent(c, start, action, message)
	event.StatusCode = status
	event.Result = result
	event.ErrorCode = errorCode
	event.Metadata = metadata
	recordAudit(c.Request.Context(), h.Logger, h.Audit, event)
}
