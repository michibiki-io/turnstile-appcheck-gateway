package audit

import (
	"math/rand"
	"strings"
	"sync"
	"time"
)

type PublicMode string

const (
	PublicModeSummary     PublicMode = "summary"
	PublicModeStep        PublicMode = "step"
	PublicModeFailureStep PublicMode = "failure_step"
)

type EventPriority string

const (
	EventPriorityCritical   EventPriority = "critical"
	EventPriorityNormal     EventPriority = "normal"
	EventPriorityBestEffort EventPriority = "best_effort"
)

type EventDecision struct {
	Persist      bool
	CountMetric  bool
	Priority     EventPriority
	Reason       string
	SampledOut   bool
	PublicAction bool
}

type PolicyConfig struct {
	PublicMode                PublicMode
	VerifySuccessSampleRate   float64
	VerifyFailureSampleRate   float64
	ExchangeSuccessSampleRate float64
	ExchangeFailureSampleRate float64
}

type Policy struct {
	cfg PolicyConfig
	mu  sync.Mutex
	rng *rand.Rand
}

func NewPolicy(cfg PolicyConfig) *Policy {
	cfg = NormalizePolicyConfig(cfg)
	return &Policy{cfg: cfg, rng: rand.New(rand.NewSource(time.Now().UnixNano()))}
}

func NormalizePolicyConfig(cfg PolicyConfig) PolicyConfig {
	if cfg.PublicMode == "" &&
		cfg.VerifySuccessSampleRate == 0 &&
		cfg.VerifyFailureSampleRate == 0 &&
		cfg.ExchangeSuccessSampleRate == 0 &&
		cfg.ExchangeFailureSampleRate == 0 {
		cfg.VerifyFailureSampleRate = 1
		cfg.ExchangeSuccessSampleRate = 1
		cfg.ExchangeFailureSampleRate = 1
	}
	switch cfg.PublicMode {
	case PublicModeSummary, PublicModeStep, PublicModeFailureStep:
	default:
		cfg.PublicMode = PublicModeFailureStep
	}
	cfg.VerifySuccessSampleRate = clampRate(cfg.VerifySuccessSampleRate)
	cfg.VerifyFailureSampleRate = clampRate(defaultRate(cfg.VerifyFailureSampleRate, 1))
	cfg.ExchangeSuccessSampleRate = clampRate(defaultRate(cfg.ExchangeSuccessSampleRate, 1))
	cfg.ExchangeFailureSampleRate = clampRate(defaultRate(cfg.ExchangeFailureSampleRate, 1))
	return cfg
}

func defaultRate(value, fallback float64) float64 {
	if value < 0 {
		return fallback
	}
	return value
}

func clampRate(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}

func (p *Policy) Decide(event Event) EventDecision {
	if p == nil {
		p = NewPolicy(PolicyConfig{})
	}
	action := strings.TrimSpace(event.Action)
	failed := event.Result != "" && event.Result != ResultSuccess
	if event.StatusCode >= 400 {
		failed = true
	}

	if isAdminAction(action) {
		return EventDecision{Persist: true, Priority: EventPriorityCritical, Reason: "admin_event"}
	}
	if action == "system.startup" {
		return EventDecision{Persist: true, Priority: EventPriorityNormal, Reason: "system_event"}
	}
	if action == "rate_limit.denied" {
		return EventDecision{Persist: true, CountMetric: true, Priority: EventPriorityCritical, Reason: "rate_limit_denied", PublicAction: true}
	}
	if action == "forwardauth.denied" {
		return EventDecision{Persist: true, Priority: EventPriorityCritical, Reason: "forwardauth_denied", PublicAction: true}
	}

	if isPublicSummaryAction(action) {
		return p.decideSummary(event, failed)
	}
	if isPublicStepAction(action) {
		return p.decideStep(event, failed)
	}

	priority := EventPriorityNormal
	if failed {
		priority = EventPriorityCritical
	}
	return EventDecision{Persist: true, Priority: priority, Reason: "default_persist"}
}

func (p *Policy) decideSummary(event Event, failed bool) EventDecision {
	rate := p.cfg.ExchangeSuccessSampleRate
	priority := EventPriorityNormal
	if isVerifyEvent(event) {
		rate = p.cfg.VerifySuccessSampleRate
		priority = EventPriorityBestEffort
	}
	if failed {
		rate = p.cfg.ExchangeFailureSampleRate
		priority = EventPriorityCritical
		if isVerifyEvent(event) {
			rate = p.cfg.VerifyFailureSampleRate
		}
	}
	persist := p.sample(rate)
	return EventDecision{
		Persist:      persist,
		CountMetric:  true,
		Priority:     priority,
		Reason:       "public_summary",
		SampledOut:   !persist,
		PublicAction: true,
	}
}

func (p *Policy) decideStep(event Event, failed bool) EventDecision {
	if failed {
		if p.cfg.PublicMode == PublicModeSummary {
			return EventDecision{Persist: false, Priority: EventPriorityNormal, Reason: "summary_mode_step_skip", SampledOut: true, PublicAction: true}
		}
		return EventDecision{Persist: true, Priority: EventPriorityCritical, Reason: "public_failed_step", PublicAction: true}
	}
	if p.cfg.PublicMode != PublicModeStep {
		return EventDecision{Persist: false, Priority: EventPriorityBestEffort, Reason: "step_skipped_by_mode", SampledOut: true, PublicAction: true}
	}
	rate := p.cfg.ExchangeSuccessSampleRate
	if isVerifyEvent(event) {
		rate = p.cfg.VerifySuccessSampleRate
	}
	persist := p.sample(rate)
	return EventDecision{Persist: persist, Priority: EventPriorityBestEffort, Reason: "public_success_step", SampledOut: !persist, PublicAction: true}
}

func (p *Policy) sample(rate float64) bool {
	if rate >= 1 {
		return true
	}
	if rate <= 0 {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.rng.Float64() < rate
}

func isAdminAction(action string) bool {
	return strings.HasPrefix(action, "admin.") || strings.HasPrefix(action, "audit.")
}

func isPublicSummaryAction(action string) bool {
	return action == "gateway.request" || action == "exchange.request" || action == "verify.request"
}

func isPublicStepAction(action string) bool {
	return action == "turnstile.verify" || action == "appcheck.exchange" || action == "appcheck.verify"
}

func isVerifyEvent(event Event) bool {
	if event.Action == "verify.request" || event.Action == "appcheck.verify" || event.Action == "forwardauth.denied" {
		return true
	}
	endpoint := strings.ToLower(event.Endpoint)
	path := strings.ToLower(event.Path)
	return strings.Contains(endpoint, "verify") || strings.Contains(path, "/verify")
}
