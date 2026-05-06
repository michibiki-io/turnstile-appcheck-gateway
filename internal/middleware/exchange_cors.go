package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// ExchangeCORS handles browser CORS for the public /exchange endpoint.
func ExchangeCORS(originAllowed func(string) bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := strings.TrimSpace(c.GetHeader("Origin"))
		if origin == "" {
			c.Next()
			return
		}

		if originAllowed != nil && !originAllowed(origin) {
			if c.Request.Method == http.MethodOptions {
				c.AbortWithStatus(http.StatusForbidden)
				return
			}
			c.Next()
			return
		}

		header := c.Writer.Header()
		addVaryHeader(header, "Origin")
		header.Set("Access-Control-Allow-Origin", origin)
		header.Set("Access-Control-Allow-Methods", "POST, OPTIONS")

		requestHeaders := strings.TrimSpace(c.GetHeader("Access-Control-Request-Headers"))
		if requestHeaders == "" {
			requestHeaders = "Content-Type"
		} else {
			addVaryHeader(header, "Access-Control-Request-Headers")
		}
		header.Set("Access-Control-Allow-Headers", requestHeaders)

		if c.Request.Method == http.MethodOptions {
			requestMethod := strings.TrimSpace(c.GetHeader("Access-Control-Request-Method"))
			if requestMethod != "" && !strings.EqualFold(requestMethod, http.MethodPost) {
				c.AbortWithStatus(http.StatusMethodNotAllowed)
				return
			}

			addVaryHeader(header, "Access-Control-Request-Method")
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}

func addVaryHeader(header http.Header, value string) {
	for _, existing := range header.Values("Vary") {
		for _, part := range strings.Split(existing, ",") {
			if strings.EqualFold(strings.TrimSpace(part), value) {
				return
			}
		}
	}
	header.Add("Vary", value)
}
