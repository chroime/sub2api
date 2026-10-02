package upstreamgovernance

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

var (
	ErrPricingGroupNotFound   = errors.New("local pricing group not found")
	ErrPricingVersionConflict = errors.New("local pricing policy or sale rate changed")
	ErrPricingInvalidGroup    = errors.New("invalid local pricing group")
)

// PricingState is loaded after the latest cost facts have been recorded. The
// sale rate is kept separately from the policy's active cost fact.
type PricingState struct {
	Policy       PricingPolicy
	CurrentSale  float64
	Observations []CostObservation
}

// PricingCommit is the single version-checked write performed by a
// coordinator. Implementations must update the policy/protection state and,
// when ApplySale is true, the local group's sale multiplier atomically.
type PricingCommit struct {
	OperationID           string
	LocalGroupID          int64
	ExpectedPolicyVersion int64
	ExpectedSale          float64
	PreviousCost          float64
	CostFactRevision      int64
	Decision              PricingDecision
	ApplySale             bool
	Protected             bool
	Reason                string
	Now                   time.Time
}

// PricingOperation is returned for audit/history rendering. Err is kept out
// of JSON so callers can map it to the project's normal error-code contract.
type PricingOperation struct {
	OperationID      string          `json:"operation_id"`
	LocalGroupID     int64           `json:"local_group_id"`
	Status           string          `json:"status"`
	Reason           string          `json:"reason"`
	Protected        bool            `json:"protected"`
	Applied          bool            `json:"applied"`
	Decision         PricingDecision `json:"decision"`
	CostFactRevision int64           `json:"cost_fact_revision"`
	Err              error           `json:"-"`
}

// PricingPersistence is intentionally narrower than the governance Store so
// existing store implementations/mocks do not need to grow in lockstep.
type PricingPersistence interface {
	LoadPricingState(context.Context, int64) (PricingState, error)
	RecordPricingCostFact(context.Context, int64, CostObservation) (int64, error)
	CommitPricing(context.Context, PricingCommit) error
}

// PricingFactRecorder is the narrow hook used by reconciliation. It is kept
// separate from PricingPersistence so existing governance stores can adopt
// trusted cost facts without implementing the complete pricing writer.
type PricingFactRecorder interface {
	RecordPricingCostFact(context.Context, int64, CostObservation) (int64, error)
}

// PricingCacheInvalidator lets the host service fan out scheduler/auth/user
// rate cache invalidation after the durable pricing transaction commits.
// Implementations should be idempotent and non-blocking.
type PricingCacheInvalidator interface {
	InvalidatePricing(context.Context, int64)
}

type PricingCoordinator struct {
	store       PricingPersistence
	invalidator PricingCacheInvalidator
	now         func() time.Time
}

func NewPricingCoordinator(store PricingPersistence) *PricingCoordinator {
	return &PricingCoordinator{store: store, now: time.Now}
}

func NewPricingCoordinatorWithInvalidator(store PricingPersistence, invalidator PricingCacheInvalidator) *PricingCoordinator {
	return &PricingCoordinator{store: store, invalidator: invalidator, now: time.Now}
}

// Recalculate persists incoming facts first, then calculates each local group
// once from all eligible sources. Input ordering and remote-site completion
// order therefore cannot overwrite a more conservative source's sale rate.
func (c *PricingCoordinator) Recalculate(ctx context.Context, localGroupIDs []int64, observations []CostObservation) ([]PricingOperation, error) {
	if c == nil || c.store == nil {
		return nil, ErrUnsupported
	}
	seen := map[int64]struct{}{}
	for _, id := range localGroupIDs {
		if id <= 0 {
			return nil, ErrPricingInvalidGroup
		}
		seen[id] = struct{}{}
	}
	if len(seen) == 0 {
		return nil, ErrPricingInvalidGroup
	}
	ids := make([]int64, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	// Stable order makes concurrent callers and audit records deterministic.
	sortInt64s(ids)
	byGroup := make(map[int64][]CostObservation, len(ids))
	for _, observation := range observations {
		id := observation.LocalGroupID
		if id == 0 && len(ids) == 1 {
			id = ids[0]
			observation.LocalGroupID = id
		}
		if _, ok := seen[id]; ok {
			byGroup[id] = append(byGroup[id], observation)
		}
	}

	result := make([]PricingOperation, 0, len(ids))
	for _, groupID := range ids {
		var revision int64
		for _, observation := range byGroup[groupID] {
			var err error
			revision, err = c.store.RecordPricingCostFact(ctx, groupID, observation)
			if err != nil {
				return nil, fmt.Errorf("record pricing cost fact for group %d: %w", groupID, err)
			}
		}
		state, err := c.store.LoadPricingState(ctx, groupID)
		if err != nil {
			return nil, err
		}
		op := c.recalculateGroup(ctx, groupID, state, revision)
		result = append(result, op)
	}
	return result, nil
}

func (c *PricingCoordinator) recalculateGroup(ctx context.Context, groupID int64, state PricingState, revision int64) PricingOperation {
	now := c.now()
	op := PricingOperation{OperationID: uuid.NewString(), LocalGroupID: groupID, Status: "prepared", CostFactRevision: revision}
	if revision == 0 {
		revision = state.Policy.CostFactRevision
		op.CostFactRevision = revision
	}
	if !state.Policy.Enabled {
		op.Status = "prepared"
		op.Reason = "policy_disabled"
		return op
	}
	cost, source, err := AggregateComparableCost(state.Observations)
	if err != nil {
		op.Status = "protected"
		op.Protected = true
		op.Reason = "unknown_cost"
		op.Decision = PricingDecision{Cost: state.Policy.ActiveCost, SourceID: state.Policy.ActiveCostSource, Protected: true, Reason: op.Reason}
		return c.commitProtection(ctx, state, op, revision, now)
	}
	decision, err := CalculatePricingTarget(state.Policy, cost)
	if err != nil {
		op.Status = "protected"
		op.Protected = true
		op.Reason = "invalid_policy"
		op.Decision = PricingDecision{Cost: cost, SourceID: source, Protected: true, Reason: op.Reason}
		return c.commitProtection(ctx, state, op, revision, now)
	}
	decision.SourceID = source
	previousCost := state.Policy.LastAutomaticCost
	if previousCost <= 0 {
		previousCost = state.Policy.ActiveCost
	}
	// The first trusted observation establishes the live cost baseline; it is
	// not an increase against the policy's frozen baseline and must not enter
	// the large-increase review path.
	if previousCost <= 0 {
		decision.Protected = false
		decision.IncreasePct = 0
	}
	apply, reason := ShouldApplyPricing(state.Policy, previousCost, cost, state.Policy.DecreaseObservedAt, now)
	if previousCost <= 0 {
		apply, reason = true, "baseline"
	}
	if state.Policy.ManualOwner {
		apply, reason = false, "manual_owner"
		decision.Protected = true
	}
	decision.Reason = reason
	op.Decision = decision
	op.Reason = reason
	op.Protected = decision.Protected
	op.Applied = apply && !decision.Protected
	if !op.Applied && reason == "decrease_stability" {
		op.Status = "prepared"
	}
	if decision.Protected {
		op.Status = "protected"
	}
	if reason == "unchanged" {
		op.Status = "prepared"
	}
	commit := PricingCommit{
		OperationID: op.OperationID, LocalGroupID: groupID, ExpectedPolicyVersion: state.Policy.Version,
		ExpectedSale: state.CurrentSale, CostFactRevision: revision, Decision: decision,
		PreviousCost: previousCost,
		ApplySale:    op.Applied, Protected: op.Protected, Reason: reason, Now: now,
	}
	if err := c.store.CommitPricing(ctx, commit); err != nil {
		op.Status = "conflict"
		op.Applied = false
		op.Err = err
		if !errors.Is(err, ErrPricingVersionConflict) {
			op.Status = "failed"
		}
		return op
	}
	if c.invalidator != nil {
		c.invalidator.InvalidatePricing(ctx, groupID)
	}
	if op.Applied {
		op.Status = "applied"
	}
	return op
}

func (c *PricingCoordinator) commitProtection(ctx context.Context, state PricingState, op PricingOperation, revision int64, now time.Time) PricingOperation {
	err := c.store.CommitPricing(ctx, PricingCommit{
		OperationID: op.OperationID, LocalGroupID: op.LocalGroupID, ExpectedPolicyVersion: state.Policy.Version,
		ExpectedSale: state.CurrentSale, CostFactRevision: revision, Decision: op.Decision,
		PreviousCost: state.Policy.ActiveCost,
		ApplySale:    false, Protected: true, Reason: op.Reason, Now: now,
	})
	if err != nil {
		op.Status = "conflict"
		op.Err = err
		if !errors.Is(err, ErrPricingVersionConflict) {
			op.Status = "failed"
		}
	} else if c.invalidator != nil {
		c.invalidator.InvalidatePricing(ctx, op.LocalGroupID)
	}
	return op
}

// Small local sorter avoids exporting a dependency from this package's pure
// pricing API.
func sortInt64s(values []int64) {
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j] < values[j-1]; j-- {
			values[j], values[j-1] = values[j-1], values[j]
		}
	}
}
