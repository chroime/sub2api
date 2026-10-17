package upstreamgovernance

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
)

const maxRechargeMinor int64 = 1_000_000_000_000

type RechargePolicy struct {
	Mode             string  `json:"mode"`
	Threshold        float64 `json:"threshold"`
	Unit             string  `json:"unit"`
	AmountMinor      int64   `json:"amount_minor"`
	Currency         string  `json:"currency"`
	DailyBudgetMinor int64   `json:"daily_budget_minor"`
	CooldownMinutes  int     `json:"cooldown_minutes"`
}

type RechargeCapability struct {
	Available bool   `json:"available"`
	Reason    string `json:"reason"`
}

type RechargeEvaluation struct {
	ID                        string     `json:"id"`
	EpisodeID                 string     `json:"episode_id,omitempty"`
	PolicyMatched             bool       `json:"policy_matched"`
	Status                    string     `json:"status"`
	Reasons                   []string   `json:"reasons"`
	ObservedAt                *time.Time `json:"observed_at"`
	EvaluatedAt               time.Time  `json:"evaluated_at"`
	Balance                   *float64   `json:"balance"`
	Unit                      string     `json:"unit"`
	AmountMinor               int64      `json:"amount_minor"`
	Currency                  string     `json:"currency"`
	DailyBudgetRemainingMinor int64      `json:"daily_budget_remaining_minor"`
}

type RechargePlanResult struct {
	Version    int64               `json:"version"`
	Policy     RechargePolicy      `json:"policy"`
	Capability RechargeCapability  `json:"capability"`
	Status     string              `json:"status"`
	Evaluation *RechargeEvaluation `json:"evaluation"`
}

// Simulation state is separate from any future financial reservation or order.
type RechargeState struct {
	WalletFingerprint string              `json:"wallet_fingerprint"`
	Low               bool                `json:"low"`
	EpisodeID         string              `json:"episode_id,omitempty"`
	Evaluation        *RechargeEvaluation `json:"evaluation,omitempty"`
}

type RechargeRecord struct {
	SiteID    int64          `json:"site_id"`
	Version   int64          `json:"version"`
	Policy    RechargePolicy `json:"policy"`
	State     RechargeState  `json:"state"`
	UpdatedAt time.Time      `json:"updated_at"`
}

type rechargeStore interface {
	GetRechargeRecord(context.Context, int64) (*RechargeRecord, error)
	SaveRechargePolicy(context.Context, *RechargeRecord, int64) error
	SaveRechargeEvaluation(context.Context, int64, int64, RechargeState) error
}

func defaultRechargePolicy(platform string) RechargePolicy {
	return RechargePolicy{Mode: "disabled", Threshold: 10, Unit: balanceUnit(platform), AmountMinor: 1000, Currency: "USD", DailyBudgetMinor: 10000, CooldownMinutes: 1440}
}

func normalizeRechargePolicy(policy RechargePolicy, platform string) (RechargePolicy, error) {
	policy.Currency = strings.ToUpper(strings.TrimSpace(policy.Currency))
	if (policy.Mode != "disabled" && policy.Mode != "plan_only") || !validRate(policy.Threshold) || policy.Unit != balanceUnit(platform) ||
		(policy.Currency != "USD" && policy.Currency != "CNY") || policy.AmountMinor <= 0 || policy.AmountMinor > maxRechargeMinor ||
		policy.DailyBudgetMinor < 0 || policy.DailyBudgetMinor > maxRechargeMinor || !validIntervalMinutes(policy.CooldownMinutes) {
		return RechargePolicy{}, ErrInvalid
	}
	return policy, nil
}

func (s *Service) rechargeWallet(site Site, snapshot *Snapshot) string {
	userID := int64(0)
	if session, err := s.session(site); err == nil {
		userID = session.UserID
	}
	unit := balanceUnit(site.Platform)
	if snapshot != nil && snapshot.SiteVersion == site.Version && snapshot.Catalog.Account != nil {
		unit = snapshot.Catalog.Account.Unit
	}
	hash := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%d\x00%s", site.BaseURL, site.Platform, userID, unit)))
	return hex.EncodeToString(hash[:])
}

func (s *Service) rechargeRecord(ctx context.Context, site Site) (*RechargeRecord, error) {
	store, ok := s.store.(rechargeStore)
	if !ok {
		return nil, ErrUnsupported
	}
	record, err := store.GetRechargeRecord(ctx, site.ID)
	if err != nil {
		return nil, err
	}
	if record == nil {
		record = &RechargeRecord{SiteID: site.ID, Policy: defaultRechargePolicy(site.Platform)}
	}
	return record, nil
}

func rechargeResult(record *RechargeRecord) *RechargePlanResult {
	status := "blocked"
	if record.Policy.Mode == "disabled" {
		status = "disabled"
	}
	return &RechargePlanResult{Version: record.Version, Policy: record.Policy, Capability: RechargeCapability{Available: false, Reason: "provider_unavailable"}, Status: status, Evaluation: record.State.Evaluation}
}

func (s *Service) RechargePlan(ctx context.Context, siteID int64) (*RechargePlanResult, error) {
	site, err := s.store.GetSite(ctx, siteID)
	if err != nil {
		return nil, err
	}
	record, err := s.rechargeRecord(ctx, *site)
	if err != nil {
		return nil, err
	}
	snapshot, err := s.latestBalanceSnapshot(ctx, siteID)
	if err != nil {
		return nil, err
	}
	// Hide another wallet's simulation immediately without mutating on a GET.
	if record.State.WalletFingerprint != s.rechargeWallet(*site, snapshot) {
		record.State = RechargeState{}
	}
	return rechargeResult(record), nil
}

func (s *Service) ConfigureRechargePlan(ctx context.Context, siteID, version int64, policy RechargePolicy) (*RechargePlanResult, error) {
	site, release, err := s.remoteSiteLock(ctx, siteID)
	if err != nil {
		return nil, err
	}
	defer release()
	policy, err = normalizeRechargePolicy(policy, site.Platform)
	if err != nil || version < 0 {
		return nil, ErrInvalid
	}
	record, err := s.rechargeRecord(ctx, *site)
	if err != nil {
		return nil, err
	}
	if record.Version != version {
		return nil, ErrConflict
	}
	// Every policy edit starts a distinct reviewed simulation, including changes
	// to the threshold, currency or unit. Existing accounts are never changed.
	record.Policy, record.State = policy, RechargeState{}
	if err = s.store.(rechargeStore).SaveRechargePolicy(ctx, record, version); err != nil {
		return nil, err
	}
	return rechargeResult(record), nil
}

func (s *Service) EvaluateRechargePlan(ctx context.Context, siteID int64) (*RechargePlanResult, error) {
	site, release, err := s.remoteSiteLock(ctx, siteID)
	if err != nil {
		return nil, err
	}
	defer release()
	snapshot, err := s.latestBalanceSnapshot(ctx, siteID)
	if err != nil {
		return nil, err
	}
	return s.evaluateRechargeLocked(ctx, *site, snapshot)
}

func (s *Service) evaluateRechargeAfterSnapshot(ctx context.Context, site Site, snapshot *Snapshot) {
	if _, ok := s.store.(rechargeStore); !ok {
		return
	}
	site.Status, site.LastError = "healthy", ""
	if _, err := s.evaluateRechargeLocked(ctx, site, snapshot); err != nil {
		slog.Warn("upstream recharge simulation failed", "site_id", site.ID, "error_code", ErrorCode(err))
	}
}

// The caller holds the site's existing advisory lock. There is deliberately no
// provider reference here: both manual and scheduled evaluation are local only.
func (s *Service) evaluateRechargeLocked(ctx context.Context, site Site, snapshot *Snapshot) (*RechargePlanResult, error) {
	record, err := s.rechargeRecord(ctx, site)
	if err != nil {
		return nil, err
	}
	if record.Policy.Mode != "plan_only" {
		return rechargeResult(record), nil
	}
	wallet := s.rechargeWallet(site, snapshot)
	state := record.State
	if state.WalletFingerprint != wallet {
		state = RechargeState{WalletFingerprint: wallet}
	}
	observation := s.observeBalance(site, snapshot)
	reasons := []string{}
	if !site.Enabled {
		reasons = append(reasons, "collection_disabled")
	}
	if site.Status == "reauth_required" {
		reasons = append(reasons, "reauth_required")
	} else if site.Status == "error" {
		reasons = append(reasons, "collection_failed")
	}
	if observation.reason != "" {
		reasons = append(reasons, observation.reason)
	}
	if record.Policy.Unit != observation.unit {
		reasons = append(reasons, "balance_unit_changed")
	}
	if observation.balance != nil && observation.reason == "" && site.Enabled && site.Status != "error" && site.Status != "reauth_required" && record.Policy.Unit == observation.unit {
		low := *observation.balance <= record.Policy.Threshold
		if low && !state.Low {
			state.EpisodeID = uuid.NewString()
		}
		if !low {
			state.EpisodeID = ""
			reasons = append(reasons, "balance_above_threshold")
		}
		state.Low = low
	}
	if record.Policy.AmountMinor > record.Policy.DailyBudgetMinor {
		reasons = append(reasons, "daily_budget_exceeded")
	}
	matched := len(reasons) == 0
	reasons = append(reasons, "provider_unavailable")
	id := state.EpisodeID
	if id == "" && state.Evaluation != nil {
		id = state.Evaluation.ID
	}
	if id == "" {
		id = uuid.NewString()
	}
	state.Evaluation = &RechargeEvaluation{ID: id, EpisodeID: state.EpisodeID, PolicyMatched: matched, Status: "blocked", Reasons: reasons,
		ObservedAt: observation.observedAt, EvaluatedAt: s.now(), Balance: observation.balance, Unit: observation.unit,
		AmountMinor: record.Policy.AmountMinor, Currency: record.Policy.Currency, DailyBudgetRemainingMinor: record.Policy.DailyBudgetMinor}
	// Simulated plans never consume financial budgets. A future executor must
	// reserve spent + reserved + uncertain amounts atomically before submission.
	if err = s.store.(rechargeStore).SaveRechargeEvaluation(ctx, site.ID, record.Version, state); err != nil {
		return nil, err
	}
	record.State = state
	return rechargeResult(record), nil
}
