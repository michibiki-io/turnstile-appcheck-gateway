package firebaseappcheck

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/oauth2"
)

func TestBuildCustomTokenClaims(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	now := time.Unix(1_700_000_000, 0)
	ttl := int64(90 * 60 * 1000)

	tok, err := BuildCustomToken(privateKey, "svc@example.com", "1:123:web:abc", now, CustomTokenOptions{TTLMillis: &ttl})
	if err != nil {
		t.Fatalf("BuildCustomToken() error = %v", err)
	}

	parsed, err := jwt.Parse(tok, func(token *jwt.Token) (interface{}, error) {
		return &privateKey.PublicKey, nil
	}, jwt.WithoutClaimsValidation())
	if err != nil {
		t.Fatalf("jwt.Parse() error = %v", err)
	}
	if !parsed.Valid {
		t.Fatal("expected token to be valid")
	}

	claims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok {
		t.Fatal("expected MapClaims")
	}
	if claims["iss"] != "svc@example.com" || claims["sub"] != "svc@example.com" {
		t.Fatalf("unexpected iss/sub claims: %v", claims)
	}
	if claims["app_id"] != "1:123:web:abc" {
		t.Fatalf("unexpected app_id claim: %v", claims["app_id"])
	}
	if claims["aud"] != customTokenAudience {
		t.Fatalf("unexpected aud claim: %v", claims["aud"])
	}
	if claims["ttl"] != "5400s" {
		t.Fatalf("unexpected ttl claim: %v", claims["ttl"])
	}
	if got := int64(claims["iat"].(float64)); got != now.Unix() {
		t.Fatalf("iat claim = %d", got)
	}
	if got := int64(claims["exp"].(float64)); got != now.Unix()+300 {
		t.Fatalf("exp claim = %d", got)
	}
}

func TestClientExchangeSuccess(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, ":exchangeCustomToken") {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer access" {
			t.Fatalf("authorization = %s", got)
		}
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request error = %v", err)
		}
		if req["limitedUse"] != true {
			t.Fatalf("expected limitedUse=true, got %v", req["limitedUse"])
		}
		if _, ok := req["customToken"].(string); !ok {
			t.Fatalf("missing customToken")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"token": "appcheck-token", "ttl": "3600s"})
	}))
	defer server.Close()

	httpClient := server.Client()
	httpClient.Transport = rewriteGoogleHostTransport{target: mustParseURL(t, server.URL), base: http.DefaultTransport}

	client, err := NewClient(
		httpClient,
		oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "access"}),
		privateKey,
		"svc@example.com",
		"projects/demo/apps/1:abc",
		"1:abc",
		CustomTokenOptions{},
		nil,
	)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	client.now = func() time.Time { return time.Unix(1_700_000_000, 0) }

	result, err := client.Exchange(context.Background(), true)
	if err != nil {
		t.Fatalf("Exchange() error = %v", err)
	}
	if result.Token != "appcheck-token" {
		t.Fatalf("Token = %s", result.Token)
	}
	if result.ExpireTimeMillis != time.Unix(1_700_000_000, 0).Add(time.Hour).UnixMilli() {
		t.Fatalf("ExpireTimeMillis = %d", result.ExpireTimeMillis)
	}
}

func TestParseDurationToMillis(t *testing.T) {
	got, err := parseDurationToMillis("3.500000000s")
	if err != nil {
		t.Fatalf("parseDurationToMillis() error = %v", err)
	}
	if got != 3500 {
		t.Fatalf("parseDurationToMillis() = %d", got)
	}
}

type rewriteGoogleHostTransport struct {
	target *url.URL
	base   http.RoundTripper
}

func (t rewriteGoogleHostTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	copyReq := req.Clone(req.Context())
	copyReq.URL.Scheme = t.target.Scheme
	copyReq.URL.Host = t.target.Host
	return t.base.RoundTrip(copyReq)
}

func mustParseURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("url.Parse() error = %v", err)
	}
	return u
}
