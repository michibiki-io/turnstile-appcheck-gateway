package handlers

import (
	"log/slog"
	"time"

	firebaseappchecksdk "firebase.google.com/go/v4/appcheck"
	"github.com/gin-gonic/gin"

	"github.com/michibiki-io/turnstile-appcheck-gateway/internal/audit"
	apperrors "github.com/michibiki-io/turnstile-appcheck-gateway/internal/errors"
)

// VerifyTokenVerifier abstracts Firebase Admin SDK App Check verification.
type VerifyTokenVerifier interface {
	VerifyToken(token string) (*firebaseappchecksdk.DecodedAppCheckToken, error)
}

// VerifyHandler implements ANY /api/v1/verify.
type VerifyHandler struct {
	Logger        *slog.Logger
	Verifier      VerifyTokenVerifier
	HeaderName    string
	SuccessStatus int
	FailureStatus int
	Audit         audit.Recorder
}

func (h *VerifyHandler) Handle(c *gin.Context) {
	start := time.Now()
	defer func() {
		recordAudit(c.Request.Context(), h.Logger, h.Audit, requestAuditEvent(c, start, "verify.request", "Verify request completed"))
	}()

	token := c.GetHeader(h.HeaderName)
	if token == "" {
		h.recordVerifyStep(c, start, "appcheck.verify", h.FailureStatus, audit.ResultFailure, "appcheck_missing", "Firebase App Check token missing")
		h.recordVerifyStep(c, start, "forwardauth.denied", h.FailureStatus, audit.ResultDenied, "forwardauth_denied", "Denied forwardAuth verification")
		apperrors.WriteError(c, apperrors.New(h.FailureStatus, "APPCHECK_INVALID", "Invalid or missing Firebase App Check token", nil))
		return
	}

	decoded, err := h.Verifier.VerifyToken(token)
	if err != nil {
		h.Logger.Info("app check verify failed", "error", err.Error())
		h.recordVerifyStep(c, start, "appcheck.verify", h.FailureStatus, audit.ResultFailure, "appcheck_invalid", "Firebase App Check token verification failed")
		h.recordVerifyStep(c, start, "forwardauth.denied", h.FailureStatus, audit.ResultDenied, "forwardauth_denied", "Denied forwardAuth verification")
		apperrors.WriteError(c, apperrors.New(h.FailureStatus, "APPCHECK_INVALID", "Invalid or missing Firebase App Check token", err))
		return
	}

	c.Header("X-AppCheck-Verified", "true")
	if decoded != nil && decoded.AppID != "" {
		c.Header("X-AppCheck-AppID", decoded.AppID)
	}
	c.Status(h.SuccessStatus)
	h.recordVerifyStep(c, start, "appcheck.verify", h.SuccessStatus, audit.ResultSuccess, "", "Firebase App Check token verified")
}

func (h *VerifyHandler) recordVerifyStep(c *gin.Context, start time.Time, action string, status int, result string, errorCode string, message string) {
	event := requestAuditEvent(c, start, action, message)
	event.StatusCode = status
	event.Result = result
	event.ErrorCode = errorCode
	event.Metadata = map[string]any{"service": "firebase_appcheck"}
	recordAudit(c.Request.Context(), h.Logger, h.Audit, event)
}
