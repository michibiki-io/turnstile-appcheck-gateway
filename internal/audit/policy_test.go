package audit

import "testing"

func TestPolicyExchangeDefaultFailureStep(t *testing.T) {
	policy := NewPolicy(PolicyConfig{})
	successSummary := policy.Decide(Event{Action: "exchange.request", Result: ResultSuccess, StatusCode: 200})
	if !successSummary.Persist || !successSummary.CountMetric {
		t.Fatalf("exchange summary should persist and count: %#v", successSummary)
	}
	successStep := policy.Decide(Event{Action: "turnstile.verify", Result: ResultSuccess, StatusCode: 200, Endpoint: "/api/v1/exchange"})
	if successStep.Persist {
		t.Fatalf("exchange success step should be skipped in failure_step mode: %#v", successStep)
	}
	failedStep := policy.Decide(Event{Action: "turnstile.verify", Result: ResultFailure, StatusCode: 400, Endpoint: "/api/v1/exchange"})
	if !failedStep.Persist || failedStep.Priority != EventPriorityCritical {
		t.Fatalf("exchange failed step should persist as critical: %#v", failedStep)
	}
}

func TestPolicyVerifySuccessSampledOutByDefault(t *testing.T) {
	policy := NewPolicy(PolicyConfig{})
	decision := policy.Decide(Event{Action: "verify.request", Result: ResultSuccess, StatusCode: 204, Endpoint: "/api/v1/verify"})
	if decision.Persist || !decision.CountMetric || decision.Priority != EventPriorityBestEffort {
		t.Fatalf("verify success should be metrics-only best effort by default: %#v", decision)
	}
}

func TestPolicyVerifyFailureRateLimitAndAdminPersist(t *testing.T) {
	policy := NewPolicy(PolicyConfig{})
	for _, event := range []Event{
		{Action: "verify.request", Result: ResultFailure, StatusCode: 401, Endpoint: "/api/v1/verify"},
		{Action: "appcheck.verify", Result: ResultFailure, StatusCode: 401, Endpoint: "/api/v1/verify"},
		{Action: "rate_limit.denied", Result: ResultDenied, StatusCode: 429, Endpoint: "/api/v1/verify"},
		{Action: "admin.dashboard.view", Result: ResultSuccess, StatusCode: 200},
		{Action: "audit.view", Result: ResultSuccess, StatusCode: 200},
	} {
		decision := policy.Decide(event)
		if !decision.Persist || decision.Priority != EventPriorityCritical {
			t.Fatalf("%s should persist as critical: %#v", event.Action, decision)
		}
	}
}

func TestCursorValidation(t *testing.T) {
	if _, err := DecodeCursor("not-base64"); err == nil {
		t.Fatal("expected invalid cursor error")
	}
}
