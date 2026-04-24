package handlers

import (
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	firebaseappchecksdk "firebase.google.com/go/v4/appcheck"
	"github.com/gin-gonic/gin"
)

func TestVerifyHandlerMissingHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &VerifyHandler{
		Logger:        slog.Default(),
		Verifier:      verifyStub{},
		HeaderName:    "X-Firebase-AppCheck",
		SuccessStatus: http.StatusNoContent,
		FailureStatus: http.StatusUnauthorized,
	}

	r := gin.New()
	r.Any("/verify", h.Handle)

	req := httptest.NewRequest(http.MethodGet, "/verify", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
}

func TestVerifyHandlerInvalidToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &VerifyHandler{
		Logger:        slog.Default(),
		Verifier:      verifyStub{err: errors.New("invalid")},
		HeaderName:    "X-Firebase-AppCheck",
		SuccessStatus: http.StatusNoContent,
		FailureStatus: http.StatusUnauthorized,
	}

	r := gin.New()
	r.Any("/verify", h.Handle)

	req := httptest.NewRequest(http.MethodPost, "/verify", nil)
	req.Header.Set("X-Firebase-AppCheck", "bad-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
}

func TestVerifyHandlerSuccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &VerifyHandler{
		Logger: slog.Default(),
		Verifier: verifyStub{token: &firebaseappchecksdk.DecodedAppCheckToken{
			AppID: "1:123:web:abc",
		}},
		HeaderName:    "X-Firebase-AppCheck",
		SuccessStatus: http.StatusNoContent,
		FailureStatus: http.StatusUnauthorized,
	}

	r := gin.New()
	r.Any("/verify", h.Handle)

	req := httptest.NewRequest(http.MethodDelete, "/verify", nil)
	req.Header.Set("X-Firebase-AppCheck", "good-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("X-AppCheck-Verified"); got != "true" {
		t.Fatalf("X-AppCheck-Verified = %s", got)
	}
	if got := w.Header().Get("X-AppCheck-AppID"); got != "1:123:web:abc" {
		t.Fatalf("X-AppCheck-AppID = %s", got)
	}
}

type verifyStub struct {
	token *firebaseappchecksdk.DecodedAppCheckToken
	err   error
}

func (s verifyStub) VerifyToken(_ string) (*firebaseappchecksdk.DecodedAppCheckToken, error) {
	return s.token, s.err
}
