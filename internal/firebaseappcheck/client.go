package firebaseappcheck

import (
	"bytes"
	"context"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/oauth2"
)

const appCheckExchangeEndpointPrefix = "https://firebaseappcheck.googleapis.com/v1/"

// Exchanger exchanges Turnstile-validated requests to Firebase App Check tokens.
type Exchanger interface {
	Exchange(ctx context.Context, limitedUse bool) (*ExchangeResult, error)
}

// ExchangeResult is the successful exchange result.
type ExchangeResult struct {
	Token            string
	ExpireTimeMillis int64
	TTLMillis        int64
}

// Client exchanges a Firebase App Check custom token through exchangeCustomToken REST API.
type Client struct {
	httpClient         *http.Client
	tokenSource        oauth2.TokenSource
	privateKeyEmail    string
	privateKey         *rsa.PrivateKey
	appResource        string
	appID              string
	customTokenOptions CustomTokenOptions
	now                func() time.Time
	logger             *slog.Logger
}

// NewClient creates a new Firebase App Check exchange client.
func NewClient(httpClient *http.Client, tokenSource oauth2.TokenSource, privateKey *rsa.PrivateKey, serviceAccountEmail, appResource, appID string, options CustomTokenOptions, logger *slog.Logger) (*Client, error) {
	if httpClient == nil {
		return nil, fmt.Errorf("http client is required")
	}
	if tokenSource == nil {
		return nil, fmt.Errorf("token source is required")
	}
	if privateKey == nil {
		return nil, fmt.Errorf("private key is required")
	}
	if strings.TrimSpace(serviceAccountEmail) == "" {
		return nil, fmt.Errorf("service account email is required")
	}
	if strings.TrimSpace(appResource) == "" {
		return nil, fmt.Errorf("app resource is required")
	}
	if strings.TrimSpace(appID) == "" {
		return nil, fmt.Errorf("app id is required")
	}
	if logger == nil {
		logger = slog.Default()
	}

	return &Client{
		httpClient:         httpClient,
		tokenSource:        tokenSource,
		privateKey:         privateKey,
		privateKeyEmail:    strings.TrimSpace(serviceAccountEmail),
		appResource:        strings.TrimSpace(appResource),
		appID:              strings.TrimSpace(appID),
		customTokenOptions: options,
		now:                time.Now,
		logger:             logger,
	}, nil
}

// Exchange creates a Firebase custom token and exchanges it for an App Check token.
func (c *Client) Exchange(ctx context.Context, limitedUse bool) (*ExchangeResult, error) {
	customToken, err := BuildCustomToken(c.privateKey, c.privateKeyEmail, c.appID, c.now(), c.customTokenOptions)
	if err != nil {
		return nil, fmt.Errorf("build custom token: %w", err)
	}

	tok, err := c.tokenSource.Token()
	if err != nil {
		return nil, fmt.Errorf("fetch oauth access token: %w", err)
	}

	reqBody := exchangeRequest{
		CustomToken: customToken,
		LimitedUse:  limitedUse,
	}
	payload, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal exchange request: %w", err)
	}

	url := appCheckExchangeEndpointPrefix + c.appResource + ":exchangeCustomToken"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create exchange request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+tok.AccessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("exchange request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read exchange response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		apiMessage := parseGoogleAPIError(body)
		if apiMessage == "" {
			apiMessage = "firebase exchange failed"
		}
		return nil, fmt.Errorf("firebase exchange failed with status %d: %s", resp.StatusCode, apiMessage)
	}

	var parsed exchangeResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("parse exchange response: %w", err)
	}
	if strings.TrimSpace(parsed.Token) == "" {
		return nil, fmt.Errorf("exchange response missing token")
	}

	ttlMillis, err := parseDurationToMillis(parsed.TTL)
	if err != nil {
		// Fallback: parse token exp if ttl is malformed.
		c.logger.Warn("firebase exchange response ttl parse failed; fallback to jwt exp", "error", err.Error())
		expMillis, expErr := parseJWTExpireTimeMillis(parsed.Token)
		if expErr != nil {
			return nil, fmt.Errorf("parse exchange ttl: %w", err)
		}
		return &ExchangeResult{
			Token:            parsed.Token,
			ExpireTimeMillis: expMillis,
			TTLMillis:        expMillis - c.now().UnixMilli(),
		}, nil
	}

	expireAt := c.now().Add(time.Duration(ttlMillis) * time.Millisecond).UnixMilli()
	return &ExchangeResult{
		Token:            parsed.Token,
		ExpireTimeMillis: expireAt,
		TTLMillis:        ttlMillis,
	}, nil
}

func parseJWTExpireTimeMillis(token string) (int64, error) {
	claims := jwt.MapClaims{}
	parser := jwt.NewParser(jwt.WithoutClaimsValidation())
	_, _, err := parser.ParseUnverified(token, claims)
	if err != nil {
		return 0, fmt.Errorf("parse token: %w", err)
	}
	rawExp, ok := claims["exp"]
	if !ok {
		return 0, fmt.Errorf("token missing exp claim")
	}
	switch v := rawExp.(type) {
	case float64:
		return int64(v) * 1000, nil
	case json.Number:
		i, convErr := v.Int64()
		if convErr != nil {
			return 0, fmt.Errorf("invalid exp claim: %w", convErr)
		}
		return i * 1000, nil
	default:
		return 0, fmt.Errorf("unexpected exp claim type %T", rawExp)
	}
}

type exchangeRequest struct {
	CustomToken string `json:"customToken"`
	LimitedUse  bool   `json:"limitedUse"`
}

type exchangeResponse struct {
	Token string `json:"token"`
	TTL   string `json:"ttl"`
}

type googleAPIErrorEnvelope struct {
	Error struct {
		Message string `json:"message"`
	} `json:"error"`
}

func parseGoogleAPIError(body []byte) string {
	var parsed googleAPIErrorEnvelope
	if err := json.Unmarshal(body, &parsed); err != nil {
		return strings.TrimSpace(string(body))
	}
	return strings.TrimSpace(parsed.Error.Message)
}
