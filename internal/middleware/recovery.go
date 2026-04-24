package middleware

import (
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/example/turnstile-appcheck-gateway/internal/model"
)

// Recovery captures panics and returns a stable JSON error payload.
func Recovery(logger *slog.Logger) gin.HandlerFunc {
	return gin.CustomRecovery(func(c *gin.Context, recovered interface{}) {
		logger.Error("panic recovered", "recover", recovered, "path", c.Request.URL.Path)
		c.AbortWithStatusJSON(http.StatusInternalServerError, model.ErrorResponse{
			Error: model.ErrorDetail{
				Code:    "INTERNAL_ERROR",
				Message: "Unexpected server error",
			},
		})
	})
}
