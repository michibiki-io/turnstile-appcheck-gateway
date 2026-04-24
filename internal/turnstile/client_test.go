package turnstile

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestClientVerifySuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s", r.Method)
		}
		if got := r.Header.Get("Content-Type"); got != "application/x-www-form-urlencoded" {
			t.Fatalf("content-type = %s", got)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm() error = %v", err)
		}
		if r.Form.Get("secret") != "secret" {
			t.Fatalf("secret form value mismatch")
		}
		if r.Form.Get("response") != "token-value" {
			t.Fatalf("response form value mismatch")
		}
		if r.Form.Get("remoteip") != "203.0.113.10" {
			t.Fatalf("remoteip form value mismatch")
		}
		_ = json.NewEncoder(w).Encode(VerifyResponse{Success: true})
	}))
	defer server.Close()

	client, err := NewClient(server.Client(), "secret", server.URL)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	resp, err := client.Verify(context.Background(), VerifyRequest{Token: "token-value", RemoteIP: "203.0.113.10"})
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if !resp.Success {
		t.Fatal("expected success=true")
	}
}

func TestClientVerifyFailureStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(VerifyResponse{Success: false, ErrorCodes: []string{"invalid-input-response"}})
	}))
	defer server.Close()

	client, err := NewClient(server.Client(), "secret", server.URL)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	_, err = client.Verify(context.Background(), VerifyRequest{Token: "bad"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestClientVerifyMissingToken(t *testing.T) {
	client, err := NewClient(http.DefaultClient, "secret", (&url.URL{Scheme: "https", Host: "example.com"}).String())
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	_, err = client.Verify(context.Background(), VerifyRequest{})
	if err == nil {
		t.Fatal("expected error")
	}
}
