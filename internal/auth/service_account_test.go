package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"strings"
	"testing"
)

func TestNormalizeJSON(t *testing.T) {
	raw := []byte(`{"client_email":"svc@example.com","private_key":"-----BEGIN PRIVATE KEY-----\\nabc\\n-----END PRIVATE KEY-----\\n"}`)
	normalized, sa, err := NormalizeJSON(raw)
	if err != nil {
		t.Fatalf("NormalizeJSON error = %v", err)
	}
	if !strings.Contains(sa.PrivateKey, "\n") {
		t.Fatalf("expected private key to contain newline")
	}
	if len(normalized) == 0 {
		t.Fatal("normalized json should not be empty")
	}
}

func TestParseRSAPrivateKey(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey error = %v", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("MarshalPKCS8PrivateKey error = %v", err)
	}
	pemKey := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})

	sa := &ServiceAccount{PrivateKey: string(pemKey), ClientEmail: "svc@example.com"}
	parsed, err := ParseRSAPrivateKey(sa)
	if err != nil {
		t.Fatalf("ParseRSAPrivateKey error = %v", err)
	}
	if parsed.N.Cmp(key.N) != 0 {
		t.Fatal("parsed private key does not match input")
	}
}
