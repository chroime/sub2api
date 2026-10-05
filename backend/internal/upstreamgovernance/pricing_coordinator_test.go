package upstreamgovernance

import (
	"context"
	"errors"
	"testing"
	"time"
)

type pricingMemoryStore struct {
	state       map[int64]PricingState
	commits     []PricingCommit
	factWrites  []CostObservation
	commitError error
}

func (s *pricingMemoryStore) LoadPricingState(_ context.Context, groupID int64) (PricingState, error) {
	v, ok := s.state[groupID]
	if !ok {
		return PricingState{}, ErrPricingGroupNotFound
	}
	return v, nil
}
func (s *pricingMemoryStore) RecordPricingCostFact(_ context.Context, groupID int64, fact CostObservation) (int64, error) {
	fact.LocalGroupID = groupID
	s.factWrites = append(s.factWrites, fact)
	state := s.state[groupID]
	replaced := false
	for i := range state.Observations {
		if state.Observations[i].SourceID == fact.SourceID {
			state.Observations[i] = fact
			replaced = true
			break
		}
	}
	if !replaced {
		state.Observations = append(state.Observations, fact)
	}
	s.state[groupID] = state
	return int64(len(s.factWrites)), nil
}
func (s *pricingMemoryStore) CommitPricing(_ context.Context, commit PricingCommit) error {
	if s.commitError != nil {
		return s.commitError
	}
	s.commits = append(s.commits, commit)
	state := s.state[commit.LocalGroupID]
	if commit.ExpectedPolicyVersion != state.Policy.Version || commit.ExpectedSale != state.CurrentSale {
		return ErrPricingVersionConflict
	}
	if commit.ApplySale {
		state.CurrentSale = commit.Decision.TargetSale
		state.Policy.LastAutomaticSale = commit.Decision.TargetSale
		state.Policy.LastAutomaticCost = commit.Decision.Cost
		state.Policy.DecreaseObservedAt = time.Time{}
	} else if commit.Reason == "decrease_stability" && state.Policy.DecreaseObservedAt.IsZero() {
		state.Policy.DecreaseObservedAt = commit.Now
	}
	state.Policy.ActiveCost = commit.Decision.Cost
	state.Policy.ActiveCostSource = commit.Decision.SourceID
	state.Policy.Protected = commit.Decision.Protected
	state.Policy.ProtectionReason = commit.Decision.Reason
	state.Policy.CostFactRevision = commit.CostFactRevision
	state.Policy.Version++
	s.state[commit.LocalGroupID] = state
	return nil
}

func newPricingMemory() *pricingMemoryStore {
	return &pricingMemoryStore{state: map[int64]PricingState{1: {
		Policy:      PricingPolicy{LocalGroupID: 1, Enabled: true, Mode: PricingModeKeepMargin, BaselineCost: 1, BaselineSale: 1.5, Version: 3, DecreaseStabilitySeconds: 60, MaxIncreasePercent: 20},
		CurrentSale: 1.5,
	}}}
}

func TestPricingCoordinatorAggregatesAllSourcesBeforeApplyingSale(t *testing.T) {
	store := newPricingMemory()
	c := NewPricingCoordinator(store)
	ops, err := c.Recalculate(context.Background(), []int64{1}, []CostObservation{
		{LocalGroupID: 1, SourceID: "cheap", Cost: 1.1, Eligible: true, Comparable: true},
		{LocalGroupID: 1, SourceID: "backup", Cost: 1.3, Eligible: true, Comparable: true},
	})
	if err != nil {
		t.Fatalf("Recalculate() error = %v", err)
	}
	if len(ops) != 1 || ops[0].Status != "applied" {
		t.Fatalf("operations = %#v, want one applied operation", ops)
	}
	if got, want := store.commits[0].Decision.Cost, 1.3; got != want {
		t.Fatalf("cost = %v, want %v", got, want)
	}
	if got, want := store.commits[0].Decision.TargetSale, 1.95; got != want {
		t.Fatalf("sale = %v, want %v", got, want)
	}
}

func TestPricingCoordinatorPersistsUnknownCostAsProtection(t *testing.T) {
	store := newPricingMemory()
	c := NewPricingCoordinator(store)
	ops, err := c.Recalculate(context.Background(), []int64{1}, []CostObservation{{LocalGroupID: 1, SourceID: "unknown", Eligible: true, Unknown: true}})
	if err != nil {
		t.Fatalf("Recalculate() error = %v", err)
	}
	if len(ops) != 1 || ops[0].Status != "protected" || !ops[0].Protected {
		t.Fatalf("operations = %#v, want protected", ops)
	}
	if len(store.commits) != 1 || store.commits[0].ApplySale {
		t.Fatalf("commits = %#v, want protection-only commit", store.commits)
	}
}

func TestPricingCoordinatorDoesNotApplyManualOrUnstableDecrease(t *testing.T) {
	store := newPricingMemory()
	store.state[1] = PricingState{Policy: PricingPolicy{LocalGroupID: 1, Enabled: true, Mode: PricingModeKeepMargin, BaselineCost: 1, BaselineSale: 1.5, Version: 3, DecreaseStabilitySeconds: 60, ManualOwner: true, ActiveCost: 1.3}, CurrentSale: 1.95}
	c := NewPricingCoordinator(store)
	ops, err := c.Recalculate(context.Background(), []int64{1}, []CostObservation{{LocalGroupID: 1, SourceID: "source", Cost: 1, Eligible: true, Comparable: true}})
	if err != nil {
		t.Fatalf("Recalculate() error = %v", err)
	}
	if ops[0].Status != "protected" || ops[0].Reason != "manual_owner" {
		t.Fatalf("operation = %#v, want manual protection", ops[0])
	}
}

func TestPricingCoordinatorPropagatesVersionConflictAndDoesNotReportApplied(t *testing.T) {
	store := newPricingMemory()
	store.commitError = ErrPricingVersionConflict
	c := NewPricingCoordinator(store)
	ops, err := c.Recalculate(context.Background(), []int64{1}, []CostObservation{{LocalGroupID: 1, SourceID: "source", Cost: 1.1, Eligible: true, Comparable: true}})
	if err != nil {
		t.Fatalf("Recalculate() should return auditable failed op, got error %v", err)
	}
	if len(ops) != 1 || ops[0].Status != "conflict" || !errors.Is(ops[0].Err, ErrPricingVersionConflict) {
		t.Fatalf("operations = %#v, want conflict", ops)
	}
}

func TestPricingCoordinatorRejectsInvalidGroupIDs(t *testing.T) {
	c := NewPricingCoordinator(newPricingMemory())
	if _, err := c.Recalculate(context.Background(), []int64{0}, nil); !errors.Is(err, ErrPricingInvalidGroup) {
		t.Fatalf("error = %v, want ErrPricingInvalidGroup", err)
	}
}

var _ = time.Time{}
