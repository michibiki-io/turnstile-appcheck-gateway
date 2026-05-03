package admin

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/michibiki-io/turnstile-appcheck-gateway/internal/audit"
	"github.com/michibiki-io/turnstile-appcheck-gateway/internal/config"
)

type Identity struct {
	User         string   `json:"user"`
	Email        string   `json:"email"`
	Groups       []string `json:"groups"`
	AuthMode     string   `json:"authMode"`
	AuthDisabled bool     `json:"authDisabled"`
}

func (h *Handler) required() gin.HandlerFunc {
	return func(c *gin.Context) {
		identity, status, allowed := IdentityFromRequest(c, h.cfg.Admin.Auth)
		if allowed {
			c.Set(identityKey, identity)
			c.Next()
			return
		}
		actor := identity.User
		if actor == "" {
			actor = "anonymous"
		}
		h.record(c.Request.Context(), audit.Event{
			Actor:       actor,
			ActorSource: "admin:" + h.cfg.Admin.Auth.Mode,
			Action:      "admin.access.denied",
			Method:      c.Request.Method,
			Path:        c.Request.URL.Path,
			Endpoint:    c.FullPath(),
			StatusCode:  status,
			Result:      audit.ResultDenied,
			RemoteAddr:  clientAddress(c),
			UserAgent:   c.Request.UserAgent(),
			RequestID:   requestID(c),
			ErrorCode:   "admin_access_denied",
			Message:     "Denied admin access",
		})
		if status == http.StatusUnauthorized {
			c.AbortWithStatusJSON(status, gin.H{"error": "admin authentication is required"})
			return
		}
		c.AbortWithStatusJSON(status, gin.H{"error": "admin authorization is required"})
	}
}

func IdentityFromRequest(c *gin.Context, auth config.AdminAuthConfig) (Identity, int, bool) {
	mode := strings.ToLower(strings.TrimSpace(auth.Mode))
	if mode == "none" {
		return Identity{User: "anonymous", AuthMode: "none", AuthDisabled: true}, http.StatusOK, true
	}
	if mode != "header" {
		return Identity{AuthMode: mode}, http.StatusForbidden, false
	}
	user := strings.TrimSpace(c.GetHeader(auth.UserHeader))
	email := strings.TrimSpace(c.GetHeader(auth.EmailHeader))
	groups := splitHeaderGroups(c.GetHeader(auth.GroupsHeader))
	if user == "" {
		user = email
	}
	identity := Identity{User: user, Email: email, Groups: groups, AuthMode: "header"}
	if user == "" {
		return identity, http.StatusUnauthorized, false
	}
	if allowed(identity, auth.AllowedUsers, auth.AllowedGroups) {
		return identity, http.StatusOK, true
	}
	return identity, http.StatusForbidden, false
}

func currentIdentity(c *gin.Context) Identity {
	value, ok := c.Get(identityKey)
	if !ok {
		return Identity{User: "anonymous", AuthMode: "none", AuthDisabled: true}
	}
	identity, ok := value.(Identity)
	if !ok {
		return Identity{User: "anonymous", AuthMode: "none", AuthDisabled: true}
	}
	if identity.User == "" {
		identity.User = "anonymous"
	}
	return identity
}

func allowed(identity Identity, users, groups []string) bool {
	userSet := map[string]struct{}{}
	for _, user := range users {
		user = strings.ToLower(strings.TrimSpace(user))
		if user != "" {
			userSet[user] = struct{}{}
		}
	}
	if _, ok := userSet[strings.ToLower(identity.User)]; ok {
		return true
	}
	if identity.Email != "" {
		if _, ok := userSet[strings.ToLower(identity.Email)]; ok {
			return true
		}
	}
	groupSet := map[string]struct{}{}
	for _, group := range groups {
		group = strings.ToLower(strings.TrimSpace(group))
		if group != "" {
			groupSet[group] = struct{}{}
		}
	}
	for _, group := range identity.Groups {
		if _, ok := groupSet[strings.ToLower(strings.TrimSpace(group))]; ok {
			return true
		}
	}
	return false
}

func splitHeaderGroups(value string) []string {
	parts := strings.Split(value, ",")
	groups := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			groups = append(groups, part)
		}
	}
	return groups
}
