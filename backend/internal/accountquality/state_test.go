package accountquality

import (
	"testing"
	"time"
)

func testPolicy() Policy {
	return Policy{ID: 1, Version: 1, Name: "标准推理", Platform: "openai", Model: "test-model", APIMode: "responses", Templates: []string{"logic", "constraint", "context"}, MaxOutputTokens: 1024, TimeoutSeconds: 120, IntervalSeconds: 600, ConfirmSeconds: 60, FailureThreshold: 2, RecoveryThreshold: 2, CooldownSeconds: 900, RecoveryIntervalSeconds: 300, DailyRequestLimit: 200, ControlMode: "auto_quarantine", IsolationScope: "model"}
}
func TestQualityIsolationAndRecoveryAreIndependent(t *testing.T) {
	now := time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC)
	p := testPolicy()
	b := Binding{AutoIsolate: true, State: "untested"}
	b, event := Advance(b, p, Run{ExecutionStatus: "completed", QualityVerdict: "fail", AutoControlEligible: true}, now)
	if b.Isolated || b.Failures != 1 || event != "suspected" {
		t.Fatalf("first failure: %+v %s", b, event)
	}
	b, _ = Advance(b, p, Run{ExecutionStatus: "timeout", QualityVerdict: "not_evaluated"}, now)
	if b.Failures != 1 {
		t.Fatal("transport error must preserve counter")
	}
	b, event = Advance(b, p, Run{ExecutionStatus: "completed", QualityVerdict: "fail", AutoControlEligible: true}, now)
	if !b.Isolated || event != "quarantined" || !b.NextRunAt.Equal(now.Add(900*time.Second)) {
		t.Fatalf("isolate: %+v %s", b, event)
	}
	b, event = Advance(b, p, Run{ExecutionStatus: "completed", QualityVerdict: "pass", AutoControlEligible: true}, now.Add(900*time.Second))
	if !b.Isolated || b.RecoveryPasses != 1 || event != "" {
		t.Fatal("one pass must not release")
	}
	b, event = Advance(b, p, Run{ExecutionStatus: "completed", QualityVerdict: "pass", AutoControlEligible: true}, now.Add(1200*time.Second))
	if b.Isolated || b.Failures != 0 || event != "recovered" {
		t.Fatalf("recover: %+v %s", b, event)
	}
}
func TestQualityNoAutomaticControlWithoutExplicitEligibility(t *testing.T) {
	for _, mode := range []string{"record_only", "record_notify", "auto_quarantine"} {
		for _, automatic := range []bool{false, true} {
			p := testPolicy()
			p.ControlMode = mode
			b := Binding{AutoIsolate: automatic, Failures: 1}
			b, _ = Advance(b, p, Run{ExecutionStatus: "completed", QualityVerdict: "fail", AutoControlEligible: false}, time.Now())
			if b.Isolated {
				t.Fatal("unsupported adapter must never isolate")
			}
		}
	}
	p := testPolicy()
	p.ControlMode = "record_notify"
	b, _ := Advance(Binding{AutoIsolate: true, Failures: 1}, p, Run{ExecutionStatus: "completed", QualityVerdict: "fail", AutoControlEligible: true}, time.Now())
	if b.Isolated {
		t.Fatal("notify mode cannot isolate")
	}
}
func TestQualityUnclearEvidencePreservesIsolationAndCounters(t *testing.T) {
	for _, status := range []string{"timeout", "cancelled", "truncated", "rate_limited", "unsupported", "budget_paused", "network_error"} {
		b, _ := Advance(Binding{Isolated: true, Failures: 2, RecoveryPasses: 1}, testPolicy(), Run{ExecutionStatus: status, QualityVerdict: "not_evaluated"}, time.Now())
		if !b.Isolated || b.Failures != 2 || b.RecoveryPasses != 1 {
			t.Fatalf("%s changed quality: %+v", status, b)
		}
	}
}
func TestQualityPolicyValidation(t *testing.T) {
	p := testPolicy()
	if err := ValidatePolicy(p); err != nil {
		t.Fatal(err)
	}
	p.FailureThreshold = 0
	if ValidatePolicy(p) == nil {
		t.Fatal("threshold must be positive")
	}
	p = testPolicy()
	p.Templates = []string{"pelican"}
	if ValidatePolicy(p) == nil {
		t.Fatal("HTML generation is not a deterministic ability test")
	}
}
func TestQualityTemplateJudgement(t *testing.T) {
	for _, id := range []string{"logic", "constraint", "context"} {
		prompt, expected, err := BuildPrompt(id)
		if err != nil || prompt == "" || expected == "" {
			t.Fatal(id, err)
		}
		verdict, _ := Judge(id, expected, expected)
		if verdict != "pass" {
			t.Fatal(id, verdict)
		}
		verdict, _ = Judge(id, expected, "I cannot determine this")
		if verdict == "pass" {
			t.Fatal("unrelated output passed")
		}
	}
}
