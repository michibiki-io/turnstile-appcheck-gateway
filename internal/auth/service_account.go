package auth

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// ServiceAccount represents the required service account credential fields.
type ServiceAccount struct {
	Type         string `json:"type"`
	ProjectID    string `json:"project_id"`
	PrivateKeyID string `json:"private_key_id"`
	PrivateKey   string `json:"private_key"`
	ClientEmail  string `json:"client_email"`
	TokenURI     string `json:"token_uri"`
}

// ParseServiceAccountJSON parses and validates service account JSON.
func ParseServiceAccountJSON(raw []byte) (*ServiceAccount, error) {
	var sa ServiceAccount
	if err := json.Unmarshal(raw, &sa); err != nil {
		return nil, fmt.Errorf("invalid service account json: %w", err)
	}

	sa.PrivateKey = strings.ReplaceAll(sa.PrivateKey, "\\n", "\n")
	if strings.TrimSpace(sa.ClientEmail) == "" {
		return nil, errors.New("service account missing client_email")
	}
	if strings.TrimSpace(sa.PrivateKey) == "" {
		return nil, errors.New("service account missing private_key")
	}

	return &sa, nil
}

// NormalizeJSON normalizes service account JSON (notably private key newlines) for downstream SDKs.
func NormalizeJSON(raw []byte) ([]byte, *ServiceAccount, error) {
	sa, err := ParseServiceAccountJSON(raw)
	if err != nil {
		return nil, nil, err
	}
	out, err := json.Marshal(sa)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to re-marshal service account json: %w", err)
	}
	return out, sa, nil
}

// NewTokenSource creates an OAuth2 token source from service account JSON.
func NewTokenSource(ctx context.Context, serviceAccountJSON []byte, scopes ...string) (oauth2.TokenSource, error) {
	cfg, err := google.JWTConfigFromJSON(serviceAccountJSON, scopes...)
	if err != nil {
		return nil, fmt.Errorf("failed to create oauth config: %w", err)
	}
	return cfg.TokenSource(ctx), nil
}

// ParseRSAPrivateKey parses a PEM RSA private key.
func ParseRSAPrivateKey(sa *ServiceAccount) (*rsa.PrivateKey, error) {
	if sa == nil {
		return nil, errors.New("service account is nil")
	}
	block, _ := pem.Decode([]byte(sa.PrivateKey))
	if block == nil {
		return nil, errors.New("failed to decode private key PEM")
	}

	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}

	pkcs8Key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse rsa private key: %w", err)
	}
	rsaKey, ok := pkcs8Key.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("private key is not rsa")
	}
	return rsaKey, nil
}
