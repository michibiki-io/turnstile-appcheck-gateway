package model

// ExchangeRequest is the request payload from frontend to exchange a Turnstile token.
type ExchangeRequest struct {
	TurnstileToken string `json:"turnstileToken"`
	LimitedUse     bool   `json:"limitedUse"`
}

// ExchangeResponse is the response payload expected by Firebase App Check CustomProvider.getToken().
type ExchangeResponse struct {
	Token            string `json:"token"`
	ExpireTimeMillis int64  `json:"expireTimeMillis"`
}

// ErrorDetail is the error payload body.
type ErrorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ErrorResponse is a stable error envelope.
type ErrorResponse struct {
	Error ErrorDetail `json:"error"`
}
