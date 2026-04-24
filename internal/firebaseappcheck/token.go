package firebaseappcheck

import (
	"crypto/rsa"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const customTokenAudience = "https://firebaseappcheck.googleapis.com/google.firebase.appcheck.v1.TokenExchangeService"

// CustomTokenOptions configures custom token generation.
type CustomTokenOptions struct {
	TTLMillis *int64
}

// BuildCustomToken creates an App Check custom token compatible with exchangeCustomToken.
func BuildCustomToken(privateKey *rsa.PrivateKey, serviceAccountEmail, appID string, now time.Time, options CustomTokenOptions) (string, error) {
	if privateKey == nil {
		return "", fmt.Errorf("private key is nil")
	}
	if serviceAccountEmail == "" {
		return "", fmt.Errorf("service account email is required")
	}
	if appID == "" {
		return "", fmt.Errorf("app id is required")
	}

	iat := now.Unix()
	claims := jwt.MapClaims{
		"iss":    serviceAccountEmail,
		"sub":    serviceAccountEmail,
		"app_id": appID,
		"aud":    customTokenAudience,
		"exp":    iat + 5*60,
		"iat":    iat,
	}
	if options.TTLMillis != nil {
		claims["ttl"] = millisecondsToDurationString(*options.TTLMillis)
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["typ"] = "JWT"

	signed, err := token.SignedString(privateKey)
	if err != nil {
		return "", fmt.Errorf("failed to sign custom token: %w", err)
	}
	return signed, nil
}

func millisecondsToDurationString(milliseconds int64) string {
	seconds := milliseconds / 1000
	nanos := (milliseconds - seconds*1000) * 1_000_000
	if nanos <= 0 {
		return fmt.Sprintf("%ds", seconds)
	}
	return fmt.Sprintf("%d.%09ds", seconds, nanos)
}

func parseDurationToMillis(duration string) (int64, error) {
	d, err := time.ParseDuration(duration)
	if err != nil {
		return 0, fmt.Errorf("invalid duration %q: %w", duration, err)
	}
	return d.Milliseconds(), nil
}
