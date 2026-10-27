package accountquality

import (
	"fmt"
	"math"
	"net/mail"
	"strings"
	"time"
)

func DefaultPolicy() Policy {
	return Policy{Templates: []string{"logic", "constraint", "context"}, MaxOutputTokens: 1024, TimeoutSeconds: 120, IntervalSeconds: 600, ConfirmSeconds: 60, FailureThreshold: 2, RecoveryThreshold: 2, CooldownSeconds: 900, RecoveryIntervalSeconds: 300, DailyRequestLimit: 200, ControlMode: "record_notify", IsolationScope: "model", APIMode: "responses", Effort: "low"}
}
func ValidatePolicy(p Policy) error {
	if strings.TrimSpace(p.Name) == "" || len(p.Name) > 200 || strings.TrimSpace(p.Model) == "" || len(p.Model) > 250 || strings.TrimSpace(p.Platform) == "" {
		return ErrInvalid
	}
	if p.MaxOutputTokens < 64 || p.MaxOutputTokens > 32768 || p.TimeoutSeconds < 5 || p.TimeoutSeconds > 600 || p.IntervalSeconds < 10 || p.IntervalSeconds > 604800 || p.ConfirmSeconds < 10 || p.ConfirmSeconds > 86400 || p.CooldownSeconds < 10 || p.CooldownSeconds > 604800 || p.RecoveryIntervalSeconds < 10 || p.RecoveryIntervalSeconds > 86400 || p.FailureThreshold < 1 || p.FailureThreshold > 100 || p.RecoveryThreshold < 1 || p.RecoveryThreshold > 100 || p.DailyRequestLimit < 1 || p.DailyRequestLimit > 10000 {
		return ErrInvalid
	}
	if p.ControlMode != "record_only" && p.ControlMode != "record_notify" && p.ControlMode != "auto_quarantine" {
		return ErrInvalid
	}
	if p.IsolationScope != "model" && p.IsolationScope != "account" {
		return ErrInvalid
	}
	if len(p.Templates) < 1 || len(p.Templates) > 4 {
		return ErrInvalid
	}
	seen := map[string]bool{}
	for _, id := range p.Templates {
		if TemplateVersion(id) == "" || seen[id] {
			return ErrInvalid
		}
		seen[id] = true
	}
	return nil
}
func ValidateSettings(s Settings) error {
	if s.Concurrency < 1 || s.Concurrency > 20 || s.DailyRequestLimit < 1 || s.DailyRequestLimit > 100000 || math.IsNaN(s.DailyCostLimitUSD) || math.IsInf(s.DailyCostLimitUSD, 0) || s.DailyCostLimitUSD < 0 || s.Notifications.ReminderSeconds < 60 || s.Notifications.ReminderSeconds > 604800 || len(s.Notifications.EmailRecipients) > 50 || len(s.Notifications.QQTargets) > 50 {
		return ErrInvalid
	}
	for _, recipient := range s.Notifications.EmailRecipients {
		a, e := mail.ParseAddress(recipient)
		if e != nil || a.Address != recipient {
			return ErrInvalid
		}
	}
	for _, target := range s.Notifications.QQTargets {
		if len(target) > 300 || !strings.Contains(target, ":GroupMessage:") {
			return fmt.Errorf("%w: QQ target must be a group UMO", ErrInvalid)
		}
	}
	for _, event := range s.Notifications.Events {
		if event != "suspected" && event != "quarantined" && event != "recovered" && event != "manual" && event != "budget_paused" {
			return ErrInvalid
		}
	}
	return nil
}
func EffectivePolicy(p Policy, o Overrides) Policy {
	if o.IntervalSeconds > 0 {
		p.IntervalSeconds = o.IntervalSeconds
	}
	if o.FailureThreshold > 0 {
		p.FailureThreshold = o.FailureThreshold
	}
	if o.RecoveryThreshold > 0 {
		p.RecoveryThreshold = o.RecoveryThreshold
	}
	if o.IsolationScope != "" {
		p.IsolationScope = o.IsolationScope
	}
	return p
}

// Advance changes only this binding's quality state. Execution failures never
// clear either quality evidence counters or isolation owned by this binding.
func Advance(b Binding, p Policy, r Run, now time.Time) (Binding, string) {
	b.LastRunAt = &now
	b.LastDurationMS = r.DurationMS
	b.QualityVerdict = r.QualityVerdict
	b.ExecutionStatus = r.ExecutionStatus
	b.Reason = r.Reason
	b.NextRunAt = now.Add(time.Duration(p.IntervalSeconds) * time.Second)
	if b.Isolated {
		b.NextRunAt = now.Add(time.Duration(p.RecoveryIntervalSeconds) * time.Second)
	}
	if r.ExecutionStatus != "completed" || r.QualityVerdict == "not_evaluated" || r.QualityVerdict == "needs_review" {
		if !b.Isolated {
			if r.ExecutionStatus == "budget_paused" {
				b.State = "budget_paused"
			} else if r.QualityVerdict == "needs_review" {
				b.State = "pending_review"
			} else {
				b.State = "execution_error"
			}
		}
		return b, ""
	}
	if r.QualityVerdict == "fail" {
		b.Failures++
		b.RecoveryPasses = 0
		event := ""
		if b.Failures == 1 {
			event = "suspected"
		}
		if b.Isolated {
			b.State = "quarantined"
			b.NextRunAt = now.Add(time.Duration(p.CooldownSeconds) * time.Second)
			return b, event
		}
		b.State = "pending_review"
		b.NextRunAt = now.Add(time.Duration(p.ConfirmSeconds) * time.Second)
		if b.Failures >= p.FailureThreshold {
			b.State = "suspected_degradation"
			if b.AutoIsolate && p.ControlMode == "auto_quarantine" && r.AutoControlEligible {
				b.Isolated = true
				b.OverrideUntil = nil
				b.State = "quarantined"
				b.NextRunAt = now.Add(time.Duration(p.CooldownSeconds) * time.Second)
				event = "quarantined"
			}
		}
		return b, event
	}
	if r.QualityVerdict == "pass" {
		if b.Isolated {
			if !r.AutoControlEligible {
				return b, ""
			}
			b.RecoveryPasses++
			b.State = "recovery_observation"
			if b.RecoveryPasses >= p.RecoveryThreshold {
				b.Isolated = false
				b.OverrideUntil = nil
				b.State = "healthy"
				b.Failures = 0
				b.RecoveryPasses = 0
				return b, "recovered"
			}
			return b, ""
		}
		wasBad := b.Failures > 0
		b.State = "healthy"
		b.Failures = 0
		b.RecoveryPasses = 0
		if wasBad {
			return b, "recovered"
		}
	}
	return b, ""
}
