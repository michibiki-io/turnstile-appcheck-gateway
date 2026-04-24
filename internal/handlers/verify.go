package handlers

import (
	"log/slog"

	firebaseappchecksdk "firebase.google.com/go/v4/appcheck"
	"github.com/gin-gonic/gin"

	apperrors "github.com/example/turnstile-appcheck-gateway/internal/errors"
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
}

func (h *VerifyHandler) Handle(c *gin.Context) {
	token := c.GetHeader(h.HeaderName)
	if token == "" {
		apperrors.WriteError(c, apperrors.New(h.FailureStatus, "APPCHECK_INVALID", "Invalid or missing Firebase App Check token", nil))
		return
	}

	decoded, err := h.Verifier.VerifyToken(token)
	if err != nil {
		h.Logger.Info("app check verify failed", "error", err.Error())
		apperrors.WriteError(c, apperrors.New(h.FailureStatus, "APPCHECK_INVALID", "Invalid or missing Firebase App Check token", err))
		return
	}

	c.Header("X-AppCheck-Verified", "true")
	if decoded != nil && decoded.AppID != "" {
		c.Header("X-AppCheck-AppID", decoded.AppID)
	}
	c.Status(h.SuccessStatus)
}
