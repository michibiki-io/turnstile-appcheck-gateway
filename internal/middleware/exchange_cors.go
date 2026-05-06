package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

var verifyCORSMethods = []string{
	http.MethodGet,
	http.MethodHead,
	http.MethodPost,
	http.MethodPut,
	http.MethodPatch,
	http.MethodDelete,
	http.MethodOptions,
}

// ExchangeCORS handles browser CORS for the public /exchange endpoint.
func ExchangeCORS(originAllowed func(string) bool) gin.HandlerFunc {
	return endpointCORS(originAllowed, corsOptions{
		allowMethods:            []string{http.MethodPost, http.MethodOptions},
		defaultAllowHeaders:     "Content-Type",
		handlePreflight:         true,
		allowForwardedPreflight: false,
		validateRequestedMethod: true,
		requirePreflightHeaders: false,
	})
}

// VerifyCORS handles browser preflight auth checks for the forwardAuth /verify endpoint.
func VerifyCORS(originAllowed func(string) bool, verifyHeaderName string) gin.HandlerFunc {
	defaultAllowHeaders := "Content-Type, X-Firebase-AppCheck"
	if strings.TrimSpace(verifyHeaderName) != "" {
		defaultAllowHeaders = "Content-Type, " + strings.TrimSpace(verifyHeaderName)
	}

	return endpointCORS(originAllowed, corsOptions{
		allowMethods:            verifyCORSMethods,
		defaultAllowHeaders:     defaultAllowHeaders,
		handlePreflight:         true,
		allowForwardedPreflight: true,
		validateRequestedMethod: true,
		requirePreflightHeaders: true,
	})
}

type corsOptions struct {
	allowMethods            []string
	defaultAllowHeaders     string
	handlePreflight         bool
	allowForwardedPreflight bool
	validateRequestedMethod bool
	requirePreflightHeaders bool
}

func endpointCORS(originAllowed func(string) bool, opts corsOptions) gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := strings.TrimSpace(c.GetHeader("Origin"))
		preflight := isCORSPreflight(c, opts)
		if origin == "" {
			if preflight {
				c.AbortWithStatus(http.StatusNoContent)
				return
			}
			c.Next()
			return
		}

		if originAllowed != nil && !originAllowed(origin) {
			if preflight {
				c.AbortWithStatus(http.StatusForbidden)
				return
			}
			c.Next()
			return
		}

		header := c.Writer.Header()
		addVaryHeader(header, "Origin")
		header.Set("Access-Control-Allow-Origin", origin)
		header.Set("Access-Control-Allow-Methods", strings.Join(opts.allowMethods, ", "))

		requestHeaders := strings.TrimSpace(c.GetHeader("Access-Control-Request-Headers"))
		if requestHeaders == "" {
			requestHeaders = opts.defaultAllowHeaders
		} else {
			addVaryHeader(header, "Access-Control-Request-Headers")
		}
		header.Set("Access-Control-Allow-Headers", requestHeaders)

		if preflight {
			requestMethod := strings.TrimSpace(c.GetHeader("Access-Control-Request-Method"))
			if requestMethod == "" {
				requestMethod = strings.TrimSpace(c.GetHeader("X-Forwarded-Method"))
			}
			if opts.validateRequestedMethod && requestMethod != "" && !methodAllowed(requestMethod, opts.allowMethods) {
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

func isCORSPreflight(c *gin.Context, opts corsOptions) bool {
	if !opts.handlePreflight {
		return false
	}
	if c.Request.Method == http.MethodOptions {
		return true
	}
	if !opts.allowForwardedPreflight || !strings.EqualFold(strings.TrimSpace(c.GetHeader("X-Forwarded-Method")), http.MethodOptions) {
		return false
	}
	if !opts.requirePreflightHeaders {
		return true
	}
	return strings.TrimSpace(c.GetHeader("Origin")) != "" && strings.TrimSpace(c.GetHeader("Access-Control-Request-Method")) != ""
}

func methodAllowed(method string, allowed []string) bool {
	for _, candidate := range allowed {
		if strings.EqualFold(strings.TrimSpace(method), candidate) {
			return true
		}
	}
	return false
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
