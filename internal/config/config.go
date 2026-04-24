package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"time"
)

const (
	defaultSubpath            = "/appcheck"
	defaultHTTPAddr           = ":8080"
	defaultTurnstileVerifyURL = "https://challenges.cloudflare.com/turnstile/v0/siteverify"
	defaultLogLevel           = "info"
	defaultRequestTimeout     = 10 * time.Second
	defaultAppCheckTokenTTL   = 30 * time.Minute
	minAppCheckTokenTTL       = 30 * time.Minute
	maxAppCheckTokenTTL       = 7 * 24 * time.Hour
	defaultVerifyHeaderName   = "X-Firebase-AppCheck"
	defaultVerifySuccess      = 204
	defaultVerifyFailure      = 401
	defaultHealthPath         = "/healthz"
	defaultReadyPath          = "/readyz"
)

// Config is the runtime configuration built from environment variables.
type Config struct {
	AppCheckSubpath string
	HTTPAddr        string

	TurnstileSecretKey     string
	TurnstileSiteVerifyURL string

	FirebaseProjectID   string
	FirebaseAppID       string
	FirebaseAppResource string

	ServiceAccountJSON []byte

	LogLevel         string
	RequestTimeout   time.Duration
	AppCheckTokenTTL time.Duration

	AllowedExchangeOrigins []string
	TrustProxyHeaders      bool

	VerifyHeaderName    string
	VerifySuccessStatus int
	VerifyFailureStatus int

	HealthPath string
	ReadyPath  string
}

// Load reads and validates configuration from environment variables.
func Load() (*Config, error) {
	env := map[string]string{}
	for _, kv := range os.Environ() {
		parts := strings.SplitN(kv, "=", 2)
		if len(parts) != 2 {
			continue
		}
		env[parts[0]] = parts[1]
	}
	return loadFromMap(env)
}

func loadFromMap(env map[string]string) (*Config, error) {
	cfg := &Config{
		AppCheckSubpath:        normalizeSubpath(getOrDefault(env, "APPCHECK_SUBPATH", defaultSubpath)),
		HTTPAddr:               strings.TrimSpace(getOrDefault(env, "HTTP_ADDR", defaultHTTPAddr)),
		TurnstileSiteVerifyURL: strings.TrimSpace(getOrDefault(env, "TURNSTILE_SITEVERIFY_URL", defaultTurnstileVerifyURL)),
		LogLevel:               strings.ToLower(strings.TrimSpace(getOrDefault(env, "LOG_LEVEL", defaultLogLevel))),
		RequestTimeout:         defaultRequestTimeout,
		AppCheckTokenTTL:       defaultAppCheckTokenTTL,
		VerifyHeaderName:       strings.TrimSpace(getOrDefault(env, "VERIFY_HEADER_NAME", defaultVerifyHeaderName)),
		VerifySuccessStatus:    defaultVerifySuccess,
		VerifyFailureStatus:    defaultVerifyFailure,
		HealthPath:             normalizeHealthPath(getOrDefault(env, "HEALTH_PATH", defaultHealthPath)),
		ReadyPath:              normalizeHealthPath(getOrDefault(env, "READY_PATH", defaultReadyPath)),
	}

	var missing []string

	cfg.TurnstileSecretKey = strings.TrimSpace(env["TURNSTILE_SECRET_KEY"])
	if cfg.TurnstileSecretKey == "" {
		missing = append(missing, "TURNSTILE_SECRET_KEY")
	}

	cfg.FirebaseProjectID = strings.TrimSpace(env["FIREBASE_PROJECT_ID"])
	cfg.FirebaseAppID = strings.TrimSpace(env["FIREBASE_APP_ID"])
	cfg.FirebaseAppResource = strings.TrimSpace(env["FIREBASE_APP_RESOURCE"])

	if cfg.FirebaseProjectID == "" {
		missing = append(missing, "FIREBASE_PROJECT_ID")
	}
	if cfg.FirebaseAppResource == "" && cfg.FirebaseAppID == "" {
		missing = append(missing, "FIREBASE_APP_ID (required when FIREBASE_APP_RESOURCE is not set)")
	}

	var err error
	cfg.ServiceAccountJSON, err = readServiceAccountJSON(env)
	if err != nil {
		missing = append(missing, err.Error())
	}

	if cfg.FirebaseAppResource == "" && cfg.FirebaseProjectID != "" && cfg.FirebaseAppID != "" {
		cfg.FirebaseAppResource = fmt.Sprintf("projects/%s/apps/%s", cfg.FirebaseProjectID, cfg.FirebaseAppID)
	}

	if timeoutRaw := strings.TrimSpace(env["REQUEST_TIMEOUT"]); timeoutRaw != "" {
		cfg.RequestTimeout, err = time.ParseDuration(timeoutRaw)
		if err != nil || cfg.RequestTimeout <= 0 {
			return nil, fmt.Errorf("invalid REQUEST_TIMEOUT: %q", timeoutRaw)
		}
	}

	if ttlRaw := strings.TrimSpace(env["APPCHECK_TOKEN_TTL"]); ttlRaw != "" {
		cfg.AppCheckTokenTTL, err = time.ParseDuration(ttlRaw)
		if err != nil {
			return nil, fmt.Errorf("invalid APPCHECK_TOKEN_TTL: %q", ttlRaw)
		}
	}
	if cfg.AppCheckTokenTTL < minAppCheckTokenTTL || cfg.AppCheckTokenTTL > maxAppCheckTokenTTL {
		return nil, fmt.Errorf("APPCHECK_TOKEN_TTL must be between %s and %s", minAppCheckTokenTTL, maxAppCheckTokenTTL)
	}

	if trustRaw := strings.TrimSpace(env["TRUST_PROXY_HEADERS"]); trustRaw != "" {
		cfg.TrustProxyHeaders, err = strconv.ParseBool(trustRaw)
		if err != nil {
			return nil, fmt.Errorf("invalid TRUST_PROXY_HEADERS: %q", trustRaw)
		}
	}

	if successRaw := strings.TrimSpace(env["VERIFY_SUCCESS_STATUS"]); successRaw != "" {
		v, parseErr := strconv.Atoi(successRaw)
		if parseErr != nil || v < 100 || v > 599 {
			return nil, fmt.Errorf("invalid VERIFY_SUCCESS_STATUS: %q", successRaw)
		}
		if v < 200 || v > 299 {
			return nil, fmt.Errorf("VERIFY_SUCCESS_STATUS must be 2xx for forwardAuth allow semantics")
		}
		cfg.VerifySuccessStatus = v
	}

	if failureRaw := strings.TrimSpace(env["VERIFY_FAILURE_STATUS"]); failureRaw != "" {
		v, parseErr := strconv.Atoi(failureRaw)
		if parseErr != nil || v < 100 || v > 599 {
			return nil, fmt.Errorf("invalid VERIFY_FAILURE_STATUS: %q", failureRaw)
		}
		if v >= 200 && v <= 299 {
			return nil, fmt.Errorf("VERIFY_FAILURE_STATUS must be non-2xx for forwardAuth deny semantics")
		}
		cfg.VerifyFailureStatus = v
	}

	if originRaw := strings.TrimSpace(env["ALLOWED_EXCHANGE_ORIGINS"]); originRaw != "" {
		cfg.AllowedExchangeOrigins, err = parseAllowedOrigins(originRaw)
		if err != nil {
			return nil, err
		}
	}

	if cfg.HTTPAddr == "" {
		cfg.HTTPAddr = defaultHTTPAddr
	}

	if cfg.VerifyHeaderName == "" {
		cfg.VerifyHeaderName = defaultVerifyHeaderName
	}

	if len(missing) > 0 {
		return nil, errors.New("missing/invalid required configuration: " + strings.Join(missing, ", "))
	}

	if !isValidAppResource(cfg.FirebaseAppResource) {
		return nil, fmt.Errorf("invalid FIREBASE_APP_RESOURCE: %q", cfg.FirebaseAppResource)
	}
	if cfg.FirebaseAppID == "" {
		cfg.FirebaseAppID = appIDFromResource(cfg.FirebaseAppResource)
	}

	return cfg, nil
}

// IsExchangeOriginAllowed returns whether origin is allowed. Empty allow-list means allow all.
func (c *Config) IsExchangeOriginAllowed(origin string) bool {
	if len(c.AllowedExchangeOrigins) == 0 {
		return true
	}
	origin = strings.TrimSpace(origin)
	if origin == "" {
		return false
	}
	normalized, err := normalizeOrigin(origin)
	if err != nil {
		return false
	}
	for _, allowed := range c.AllowedExchangeOrigins {
		if allowed == normalized {
			return true
		}
	}
	return false
}

func getOrDefault(env map[string]string, key, fallback string) string {
	if value, ok := env[key]; ok && strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
}

func normalizeSubpath(input string) string {
	cleaned := strings.TrimSpace(input)
	if cleaned == "" {
		cleaned = defaultSubpath
	}
	if !strings.HasPrefix(cleaned, "/") {
		cleaned = "/" + cleaned
	}
	cleaned = path.Clean(cleaned)
	if cleaned == "." {
		return "/"
	}
	if cleaned != "/" {
		cleaned = strings.TrimSuffix(cleaned, "/")
	}
	return cleaned
}

func normalizeHealthPath(input string) string {
	p := strings.TrimSpace(input)
	if p == "" {
		return "/"
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return path.Clean(p)
}

func parseAllowedOrigins(raw string) ([]string, error) {
	parts := strings.Split(raw, ",")
	origins := make([]string, 0, len(parts))
	seen := map[string]struct{}{}
	for _, part := range parts {
		v := strings.TrimSpace(part)
		if v == "" {
			continue
		}
		normalized, err := normalizeOrigin(v)
		if err != nil {
			return nil, fmt.Errorf("invalid ALLOWED_EXCHANGE_ORIGINS value %q: %w", v, err)
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		origins = append(origins, normalized)
	}
	return origins, nil
}

func normalizeOrigin(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", err
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return "", fmt.Errorf("scheme must be http or https")
	}
	if u.Host == "" {
		return "", fmt.Errorf("missing host")
	}
	if u.Path != "" && u.Path != "/" {
		return "", fmt.Errorf("path is not allowed")
	}
	if u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return "", fmt.Errorf("origin must not include query, fragment or userinfo")
	}
	return strings.ToLower(u.Scheme + "://" + u.Host), nil
}

func isValidAppResource(resource string) bool {
	parts := strings.Split(resource, "/")
	if len(parts) != 4 {
		return false
	}
	return parts[0] == "projects" && parts[2] == "apps" && parts[1] != "" && parts[3] != ""
}

func appIDFromResource(resource string) string {
	parts := strings.Split(resource, "/")
	if len(parts) != 4 {
		return ""
	}
	return parts[3]
}

func readServiceAccountJSON(env map[string]string) ([]byte, error) {
	base64Value := strings.TrimSpace(env["GOOGLE_SERVICE_ACCOUNT_JSON_BASE64"])
	if base64Value != "" {
		decoded, err := base64.StdEncoding.DecodeString(base64Value)
		if err != nil {
			return nil, fmt.Errorf("GOOGLE_SERVICE_ACCOUNT_JSON_BASE64 is invalid base64")
		}
		if len(decoded) == 0 {
			return nil, fmt.Errorf("GOOGLE_SERVICE_ACCOUNT_JSON_BASE64 is empty")
		}
		return decoded, nil
	}

	raw := env["GOOGLE_SERVICE_ACCOUNT_JSON"]
	if strings.TrimSpace(raw) == "" {
		return nil, fmt.Errorf("GOOGLE_SERVICE_ACCOUNT_JSON or GOOGLE_SERVICE_ACCOUNT_JSON_BASE64 is required")
	}
	if strings.TrimSpace(raw) == "" {
		return nil, fmt.Errorf("GOOGLE_SERVICE_ACCOUNT_JSON is empty")
	}
	return []byte(raw), nil
}
