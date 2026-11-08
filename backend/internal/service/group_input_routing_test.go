package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/stretchr/testify/require"
)

type inputRoutingGroupRepo struct {
	GroupRepository
	groups map[int64]*Group
}

func (r inputRoutingGroupRepo) GetByIDLite(_ context.Context, id int64) (*Group, error) {
	if group := r.groups[id]; group != nil {
		return group, nil
	}
	return nil, ErrGroupNotFound
}

type inputRoutingAccountRepo struct{ stubOpenAIAccountRepo }

func (r inputRoutingAccountRepo) ListSchedulableByGroupIDAndPlatform(ctx context.Context, id int64, platform string) ([]Account, error) {
	var accounts []Account
	for _, account := range r.accounts {
		if account.Platform == platform && openAIStickyAccountMatchesGroup(&account, &id) {
			accounts = append(accounts, account)
		}
	}
	return accounts, nil
}

func (r inputRoutingAccountRepo) ListSchedulableByGroupID(_ context.Context, id int64) ([]Account, error) {
	var accounts []Account
	for _, account := range r.accounts {
		if openAIStickyAccountMatchesGroup(&account, &id) {
			accounts = append(accounts, account)
		}
	}
	return accounts, nil
}

func (r inputRoutingAccountRepo) ListSchedulableByGroupIDAndPlatforms(ctx context.Context, id int64, platforms []string) ([]Account, error) {
	var accounts []Account
	for _, platform := range platforms {
		part, _ := r.ListSchedulableByGroupIDAndPlatform(ctx, id, platform)
		accounts = append(accounts, part...)
	}
	return accounts, nil
}

func inputRoutingGroup(rule GroupInputRoutingRule) *Group {
	return &Group{ID: 10, Platform: PlatformOpenAI, Status: StatusActive, Hydrated: true,
		InputRouting: GroupInputRoutingConfig{Enabled: true, Rules: []GroupInputRoutingRule{rule}}}
}

func inputRoutingRule() GroupInputRoutingRule {
	return GroupInputRoutingRule{ID: "small", Enabled: true, RequestType: "all", MinInputTokens: 0, MaxInputTokens: 100, AccountIDs: []int64{2}}
}

func inputRoutingAccount(id, groupID int64) Account {
	return Account{ID: id, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive,
		Schedulable: true, Concurrency: 3, Priority: int(id), GroupIDs: []int64{groupID}, AccountGroups: []AccountGroup{{GroupID: groupID}}}
}

func TestGroupInputRoutingFirstMatchAndBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name   string
		tokens int
		stream bool
		kind   string
		match  bool
	}{
		{"zero is inclusive", 0, false, "all", true},
		{"sync", 99, false, "sync", true}, {"sync excludes stream", 99, true, "sync", false},
		{"stream", 99, true, "stream", true}, {"stream excludes sync", 99, false, "stream", false},
		{"all sync", 99, false, "all", true}, {"all stream", 99, true, "all", true},
		{"upper bound is inclusive", 100, false, "all", true}, {"above upper bound", 101, false, "all", false},
		{"unknown", -1, false, "all", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rule := inputRoutingRule()
			rule.RequestType = tc.kind
			group := inputRoutingGroup(rule)
			ctx := WithGroupInputRoutingRequest(context.Background(), group, tc.tokens, tc.stream)
			decision, ok := GroupInputRoutingDecisionFromContext(ctx)
			require.Equal(t, tc.match, ok)
			if ok {
				require.Equal(t, "small", decision.RuleID)
				require.Equal(t, group.ID, decision.SourceGroupID)
			}
		})
	}
	group := inputRoutingGroup(inputRoutingRule())
	disabled := inputRoutingRule()
	disabled.ID, disabled.Enabled = "disabled", false
	second := inputRoutingRule()
	second.ID = "second"
	group.InputRouting.Rules = []GroupInputRoutingRule{disabled, group.InputRouting.Rules[0], second}
	ctx := WithGroupInputRoutingRequest(context.Background(), group, 50, false)
	group.InputRouting.Rules[1].AccountIDs[0] = 99
	decision, ok := GroupInputRoutingDecisionFromContext(ctx)
	require.True(t, ok)
	require.Equal(t, "small", decision.RuleID)
	require.Equal(t, []int64{2}, decision.TargetAccountIDs)
}

func TestGroupInputRoutingInclusiveAdjacentRangesAndUnmatchedSource(t *testing.T) {
	first := inputRoutingRule()
	first.MaxInputTokens = 100
	second := inputRoutingRule()
	second.ID, second.MinInputTokens, second.MaxInputTokens = "second", 101, 200
	group := inputRoutingGroup(first)
	group.InputRouting.Rules = []GroupInputRoutingRule{first, second}

	for _, tc := range []struct {
		tokens  int
		wantID  string
		matched bool
	}{
		{0, "small", true}, {100, "small", true}, {101, "second", true}, {200, "second", true}, {201, "", false},
	} {
		ctx := WithGroupInputRoutingRequest(context.Background(), group, tc.tokens, false)
		decision, matched := GroupInputRoutingDecisionFromContext(ctx)
		require.Equal(t, tc.matched, matched, "input tokens %d", tc.tokens)
		if matched {
			require.Equal(t, tc.wantID, decision.RuleID)
		}
	}

	source := context.WithValue(context.Background(), ctxkey.Group, group)
	unmatched := WithGroupInputRoutingRequest(source, group, 201, false)
	require.Same(t, source, unmatched, "unmatched input must retain ordinary source-group scheduling")
	_, matched := GroupInputRoutingDecisionFromContext(unmatched)
	require.False(t, matched)
}

func TestGroupInputRoutingAllTokenLengthsHonorsRequestTypeAndPriority(t *testing.T) {
	for _, tc := range []struct {
		name    string
		tokens  int
		stream  bool
		kind    string
		matched bool
	}{
		{"small sync", 20, false, "sync", true},
		{"large sync", 20_000_000, false, "sync", true},
		{"zero", 0, false, "all", true},
		{"all stream", 50_000, true, "all", true},
		{"stream rule", 50_000, true, "stream", true},
		{"sync excludes stream", 20, true, "sync", false},
		{"stream excludes sync", 20, false, "stream", false},
		{"unknown stays excluded", -1, false, "all", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rule := inputRoutingRule()
			rule.InputTokensMode, rule.RequestType = "all", tc.kind
			rule.MinInputTokens, rule.MaxInputTokens = 500, 600
			ctx := WithGroupInputRoutingRequest(context.Background(), inputRoutingGroup(rule), tc.tokens, tc.stream)
			decision, matched := GroupInputRoutingDecisionFromContext(ctx)
			require.Equal(t, tc.matched, matched)
			if matched {
				require.Equal(t, "small", decision.RuleID)
				require.Equal(t, tc.tokens, decision.EstimatedInputTokens)
			}
		})
	}

	rangeRule := inputRoutingRule()
	rangeRule.ID, rangeRule.InputTokensMode = "range", "range"
	allRule := inputRoutingRule()
	allRule.ID, allRule.InputTokensMode = "all-lengths", "all"
	for _, tc := range []struct {
		rules  []GroupInputRoutingRule
		tokens int
		want   string
	}{
		{[]GroupInputRoutingRule{rangeRule, allRule}, 50, "range"},
		{[]GroupInputRoutingRule{rangeRule, allRule}, 500, "all-lengths"},
		{[]GroupInputRoutingRule{allRule, rangeRule}, 50, "all-lengths"},
	} {
		group := inputRoutingGroup(rangeRule)
		group.InputRouting.Rules = tc.rules
		ctx := WithGroupInputRoutingRequest(context.Background(), group, tc.tokens, false)
		decision, matched := GroupInputRoutingDecisionFromContext(ctx)
		require.True(t, matched)
		require.Equal(t, tc.want, decision.RuleID)
	}
}

func TestGroupInputRoutingRangeSelectsTargetsAndUnmatchedSourceAccounts(t *testing.T) {
	for _, scheduler := range []string{"gateway", "openai"} {
		for _, stream := range []bool{false, true} {
			for _, tc := range []struct {
				tokens int
				wantID int64
			}{
				{99, 1}, {100, 2}, {200, 2}, {201, 1},
			} {
				rule := inputRoutingRule()
				rule.MinInputTokens, rule.MaxInputTokens = 100, 200
				group := inputRoutingGroup(rule)
				group.RateMultiplier = 0.35
				repo := inputRoutingAccountRepo{stubOpenAIAccountRepo{accounts: []Account{inputRoutingAccount(1, 10), inputRoutingAccount(2, 20)}}}
				groups := inputRoutingGroupRepo{groups: map[int64]*Group{10: group}}
				cfg := &config.Config{RunMode: config.RunModeStandard}
				gateway := &GatewayService{accountRepo: repo, groupRepo: groups, cfg: cfg}
				openai := &OpenAIGatewayService{accountRepo: repo, cfg: cfg}
				ctx := context.WithValue(context.Background(), ctxkey.Group, group)
				ctx = WithGroupInputRoutingRequest(ctx, group, tc.tokens, stream)
				var selected *Account
				var err error
				if scheduler == "gateway" {
					selected, err = gateway.SelectAccountForModelWithExclusions(ctx, &group.ID, "", "", nil)
				} else {
					selected, err = openai.SelectAccountForModelWithExclusions(ctx, &group.ID, "", "", nil)
				}
				require.NoError(t, err, "%s tokens=%d stream=%t", scheduler, tc.tokens, stream)
				require.Equal(t, tc.wantID, selected.ID, "%s tokens=%d stream=%t", scheduler, tc.tokens, stream)
				require.Same(t, group, ctx.Value(ctxkey.Group))
				require.Equal(t, 0.35, group.RateMultiplier)
			}
		}
	}
}

func TestGroupInputRoutingSchedulersStrictScopeAndFallback(t *testing.T) {
	for _, schedulerName := range []string{"gateway legacy", "gateway load", "openai legacy", "openai load", "openai advanced"} {
		for _, fallback := range []bool{false, true} {
			t.Run(schedulerName+map[bool]string{false: " strict", true: " fallback"}[fallback], func(t *testing.T) {
				rule := inputRoutingRule()
				rule.FallbackToOriginal = fallback
				group := inputRoutingGroup(rule)
				accounts := []Account{inputRoutingAccount(1, 10), inputRoutingAccount(2, 20)}
				repo := inputRoutingAccountRepo{stubOpenAIAccountRepo{accounts: accounts}}
				groups := inputRoutingGroupRepo{groups: map[int64]*Group{10: group}}
				cache := &schedulerTestGatewayCache{sessionBindings: map[string]int64{"session": 1, "openai:session": 1}}
				cfg := &config.Config{RunMode: config.RunModeStandard}
				cfg.Gateway.Scheduling.LoadBatchEnabled = true
				concurrency := NewConcurrencyService(schedulerTestConcurrencyCache{})
				gateway := &GatewayService{accountRepo: repo, groupRepo: groups, cache: cache, cfg: cfg, concurrencyService: concurrency}
				openai := &OpenAIGatewayService{accountRepo: repo, cache: cache, cfg: cfg, concurrencyService: concurrency}
				ctx := context.WithValue(context.Background(), ctxkey.Group, group)
				ctx = WithGroupInputRoutingRequest(ctx, group, 50, false)
				selectAccount := func(id *int64, excluded map[int64]struct{}) (*Account, error) {
					switch schedulerName {
					case "gateway legacy":
						return gateway.SelectAccountForModelWithExclusions(ctx, id, "session", "", excluded)
					case "gateway load":
						result, err := gateway.SelectAccountWithLoadAwareness(ctx, id, "session", "", excluded, "", 0)
						if result != nil {
							if result.ReleaseFunc != nil {
								result.ReleaseFunc()
							}
							return result.Account, err
						}
						return nil, err
					case "openai legacy":
						return openai.SelectAccountForModelWithExclusions(ctx, id, "session", "", excluded)
					case "openai load":
						result, err := openai.SelectAccountWithLoadAwareness(ctx, id, "session", "", excluded)
						if result != nil {
							if result.ReleaseFunc != nil {
								result.ReleaseFunc()
							}
							return result.Account, err
						}
						return nil, err
					default:
						result, _, err := newDefaultOpenAIAccountScheduler(openai, newOpenAIAccountRuntimeStats()).Select(ctx, OpenAIAccountScheduleRequest{GroupID: id, Platform: PlatformOpenAI, SessionHash: "session", ExcludedIDs: excluded})
						if result != nil {
							if result.ReleaseFunc != nil {
								result.ReleaseFunc()
							}
							return result.Account, err
						}
						return nil, err
					}
				}
				selected, err := selectAccount(&group.ID, nil)
				require.NoError(t, err)
				require.Equal(t, int64(2), selected.ID, "stale sticky and priority must not escape target pool")
				require.Equal(t, []int64{20}, selected.GroupIDs, "routing must preserve real account membership")
				require.Same(t, group, ctx.Value(ctxkey.Group), "billing group must remain the source")
				selected, err = selectAccount(&group.ID, map[int64]struct{}{2: {}})
				if fallback {
					require.NoError(t, err)
					require.Equal(t, int64(1), selected.ID)
				} else {
					require.ErrorIs(t, err, ErrNoAvailableAccounts)
					require.Nil(t, selected)
				}
				foreign := int64(20)
				selected, err = selectAccount(&foreign, nil)
				require.ErrorIs(t, err, ErrNoAvailableAccounts, "changing group on a retry must not disable the rule")
				require.Nil(t, selected)
			})
		}
	}
}

func TestGroupInputRoutingGroupTargetsUnionAndPolicy(t *testing.T) {
	for _, schedulerName := range []string{"gateway", "openai legacy", "openai advanced"} {
		for _, tc := range []struct {
			name      string
			direct    []int64
			configure func(*Group, *Group)
			wantIDs   []int64
		}{
			{"group only", nil, nil, []int64{2, 3}},
			{"deduplicated union", []int64{2, 4, 2}, nil, []int64{2, 3, 4}},
			{"inactive target", nil, func(_, target *Group) { target.Status = "disabled" }, nil},
			{"target platform does not restrict actual accounts", nil, func(_, target *Group) { target.Platform = PlatformAnthropic }, []int64{2, 3}},
			{"target changed to composite", nil, func(_, target *Group) { target.Platform = PlatformComposite }, []int64{2, 3}},
			{"composite source and target", nil, func(source, target *Group) { source.Platform, target.Platform = PlatformComposite, PlatformComposite }, []int64{2, 3}},
			{"target model denied", nil, func(_, target *Group) {
				target.ModelAllowlist = GroupModelAllowlist{Enabled: true, Models: []string{"gpt-denied"}}
			}, nil},
			{"target privacy", nil, func(_, target *Group) { target.RequirePrivacySet = true }, []int64{3}},
			{"target oauth", nil, func(_, target *Group) { target.RequireOAuthOnly = true }, []int64{3}},
			{"source privacy applies to direct", []int64{4}, func(source, _ *Group) { source.RequirePrivacySet = true }, []int64{3}},
			{"direct union bypasses target policy only", []int64{2}, func(_, target *Group) { target.RequireOAuthOnly = true }, []int64{2, 3}},
		} {
			t.Run(schedulerName+"/"+tc.name, func(t *testing.T) {
				rule := inputRoutingRule()
				rule.AccountIDs, rule.GroupIDs = tc.direct, []int64{20, 20, 999}
				group := inputRoutingGroup(rule)
				group.ModelRoutingEnabled = true
				group.ModelRouting = map[string][]int64{"gpt-5": {1}}
				target := &Group{ID: 20, Platform: PlatformOpenAI, Status: StatusActive,
					InputRouting: GroupInputRoutingConfig{Enabled: true, Rules: []GroupInputRoutingRule{{ID: "must not recurse", Enabled: true, RequestType: "all", MaxInputTokens: 100, AccountIDs: []int64{1}}}}}
				if tc.configure != nil {
					tc.configure(group, target)
				}
				accounts := []Account{inputRoutingAccount(1, 10), inputRoutingAccount(2, 20), inputRoutingAccount(3, 20), inputRoutingAccount(4, 40)}
				accounts[2].Type = AccountTypeOAuth
				accounts[2].Extra = map[string]any{"privacy_mode": PrivacyModeTrainingOff}
				repo := inputRoutingAccountRepo{stubOpenAIAccountRepo{accounts: accounts}}
				groups := inputRoutingGroupRepo{groups: map[int64]*Group{10: group, 20: target}}
				cache := &schedulerTestGatewayCache{}
				gateway := &GatewayService{accountRepo: repo, groupRepo: groups, cache: cache}
				openai := &OpenAIGatewayService{accountRepo: repo, cache: cache, schedulerSnapshot: &SchedulerSnapshotService{accountRepo: repo, groupRepo: groups}}
				ctx := WithGroupInputRoutingRequest(context.WithValue(context.Background(), ctxkey.Group, group), group, 50, false)
				if group.Platform == PlatformComposite {
					ctx = WithResolvedTargetPlatform(ctx, PlatformOpenAI)
				}
				excluded := make(map[int64]struct{})
				var got []int64
				for attempt := 0; attempt <= len(accounts); attempt++ {
					var account *Account
					var err error
					switch schedulerName {
					case "gateway":
						account, err = gateway.SelectAccountForModelWithExclusions(ctx, &group.ID, "", "gpt-5", excluded)
					case "openai legacy":
						account, err = openai.SelectAccountForModelWithExclusions(ctx, &group.ID, "", "gpt-5", excluded)
					default:
						var selection *AccountSelectionResult
						selection, _, err = newDefaultOpenAIAccountScheduler(openai, newOpenAIAccountRuntimeStats()).Select(ctx, OpenAIAccountScheduleRequest{GroupID: &group.ID, Platform: PlatformOpenAI, RequestedModel: "gpt-5", ExcludedIDs: excluded})
						if selection != nil {
							account = selection.Account
							if selection.ReleaseFunc != nil {
								selection.ReleaseFunc()
							}
						}
					}
					if err != nil {
						require.ErrorIs(t, err, ErrNoAvailableAccounts)
						break
					}
					require.NotNil(t, account)
					_, duplicate := excluded[account.ID]
					require.False(t, duplicate)
					excluded[account.ID] = struct{}{}
					got = append(got, account.ID)
				}
				require.ElementsMatch(t, tc.wantIDs, got)
			})
		}
	}
}

type inputRoutingChangingSnapshot struct {
	openAISnapshotCacheStub
	calls   int
	removed *Account
}

func (s *inputRoutingChangingSnapshot) GetAccount(ctx context.Context, id int64) (*Account, error) {
	if id == s.removed.ID {
		s.calls++
		if s.calls >= 2 {
			return s.removed, nil
		}
	}
	return s.openAISnapshotCacheStub.GetAccount(ctx, id)
}

func TestGroupInputRoutingMembershipChangeTriesRemainingTargetsBeforeFallback(t *testing.T) {
	for _, schedulerName := range []string{"gateway legacy", "gateway load", "openai legacy", "openai load"} {
		t.Run(schedulerName, func(t *testing.T) {
			rule := inputRoutingRule()
			rule.AccountIDs, rule.GroupIDs, rule.FallbackToOriginal = nil, []int64{20}, true
			group := inputRoutingGroup(rule)
			target := &Group{ID: 20, Platform: PlatformOpenAI, Status: StatusActive}
			original, first, second := inputRoutingAccount(1, 10), inputRoutingAccount(2, 20), inputRoutingAccount(3, 20)
			removed := inputRoutingAccount(2, 30)
			repo := inputRoutingAccountRepo{stubOpenAIAccountRepo{accounts: []Account{original, first, second}}}
			groups := inputRoutingGroupRepo{groups: map[int64]*Group{10: group, 20: target}}
			snapshotCache := &inputRoutingChangingSnapshot{openAISnapshotCacheStub: openAISnapshotCacheStub{snapshotAccounts: []*Account{&original, &first, &second}, accountsByID: map[int64]*Account{1: &original, 2: &first, 3: &second}}, removed: &removed}
			if schedulerName == "gateway legacy" || schedulerName == "gateway load" {
				snapshotCache.calls = 1
			}
			snapshot := &SchedulerSnapshotService{cache: snapshotCache, accountRepo: repo, groupRepo: groups}
			var acquired, released []int64
			concurrency := NewConcurrencyService(schedulerTestConcurrencyCache{acquiredIDs: &acquired, releasedIDs: &released})
			cfg := &config.Config{}
			cfg.Gateway.Scheduling.LoadBatchEnabled = true
			gateway := &GatewayService{accountRepo: repo, groupRepo: groups, schedulerSnapshot: snapshot, cfg: cfg, concurrencyService: concurrency}
			openai := &OpenAIGatewayService{accountRepo: repo, schedulerSnapshot: snapshot, cfg: cfg, concurrencyService: concurrency}
			ctx := WithGroupInputRoutingRequest(context.WithValue(context.Background(), ctxkey.Group, group), group, 50, false)
			var selected *Account
			var selection *AccountSelectionResult
			var err error
			switch schedulerName {
			case "gateway legacy":
				selected, err = gateway.SelectAccountForModelWithExclusions(ctx, &group.ID, "", "gpt-5", nil)
			case "gateway load":
				selection, err = gateway.SelectAccountWithLoadAwareness(ctx, &group.ID, "", "gpt-5", nil, "", 0)
			case "openai legacy":
				selected, err = openai.SelectAccountForModelWithExclusions(ctx, &group.ID, "", "gpt-5", nil)
			case "openai load":
				selection, err = openai.SelectAccountWithLoadAwareness(ctx, &group.ID, "", "gpt-5", nil)
			}
			if selection != nil {
				selected = selection.Account
				if selection.ReleaseFunc != nil {
					selection.ReleaseFunc()
				}
			}
			require.NoError(t, err)
			require.Equal(t, int64(3), selected.ID, "healthy remaining target takes precedence over the original group")
			require.ElementsMatch(t, acquired, released, "every acquired slot must be released once")
		})
	}
}

func TestGroupInputRoutingRejectsMembershipChangeDuringHydration(t *testing.T) {
	rule := inputRoutingRule()
	rule.AccountIDs, rule.GroupIDs = nil, []int64{20}
	group := inputRoutingGroup(rule)
	target := &Group{ID: 20, Platform: PlatformOpenAI, Status: StatusActive}
	account := inputRoutingAccount(2, 20)
	removed := inputRoutingAccount(2, 30)
	repo := inputRoutingAccountRepo{stubOpenAIAccountRepo{accounts: []Account{account}}}
	groups := inputRoutingGroupRepo{groups: map[int64]*Group{10: group, 20: target}}
	snapshotCache := &inputRoutingChangingSnapshot{openAISnapshotCacheStub: openAISnapshotCacheStub{snapshotAccounts: []*Account{&account}, accountsByID: map[int64]*Account{2: &account}}, removed: &removed}
	svc := &OpenAIGatewayService{accountRepo: repo, schedulerSnapshot: &SchedulerSnapshotService{cache: snapshotCache, accountRepo: repo, groupRepo: groups}}
	ctx := WithGroupInputRoutingRequest(context.WithValue(context.Background(), ctxkey.Group, group), group, 50, false)
	selected, err := svc.SelectAccountForModelWithExclusions(ctx, &group.ID, "", "gpt-5", nil)
	require.ErrorIs(t, err, ErrNoAvailableAccounts)
	require.Nil(t, selected)
}

func TestGroupInputRoutingOriginalFallbackRetainsMixedScheduling(t *testing.T) {
	for _, loadAware := range []bool{false, true} {
		t.Run(map[bool]string{false: "legacy", true: "load"}[loadAware], func(t *testing.T) {
			rule := inputRoutingRule()
			rule.FallbackToOriginal = true
			group := inputRoutingGroup(rule)
			group.Platform = PlatformAnthropic
			original := inputRoutingAccount(1, 10)
			original.Platform = PlatformAntigravity
			original.Extra = map[string]any{"mixed_scheduling": true}
			target := inputRoutingAccount(2, 20)
			target.Platform, target.Schedulable = PlatformAnthropic, false
			repo := inputRoutingAccountRepo{stubOpenAIAccountRepo{accounts: []Account{original, target}}}
			groups := inputRoutingGroupRepo{groups: map[int64]*Group{10: group}}
			cfg := &config.Config{}
			cfg.Gateway.Scheduling.LoadBatchEnabled = true
			svc := &GatewayService{accountRepo: repo, groupRepo: groups, cache: &schedulerTestGatewayCache{}, cfg: cfg, concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{})}
			ctx := WithGroupInputRoutingRequest(context.WithValue(context.Background(), ctxkey.Group, group), group, 50, false)
			var selected *Account
			var err error
			if loadAware {
				var selection *AccountSelectionResult
				selection, err = svc.SelectAccountWithLoadAwareness(ctx, &group.ID, "", "", nil, "", 0)
				if selection != nil {
					selected = selection.Account
					if selection.ReleaseFunc != nil {
						selection.ReleaseFunc()
					}
				}
			} else {
				selected, err = svc.SelectAccountForModelWithExclusions(ctx, &group.ID, "", "", nil)
			}
			require.NoError(t, err)
			require.Equal(t, int64(1), selected.ID)
		})
	}
}

func TestGroupInputRoutingUsesSourceProfitGate(t *testing.T) {
	for _, schedulerName := range []string{"gateway", "openai"} {
		t.Run(schedulerName, func(t *testing.T) {
			group := inputRoutingGroup(inputRoutingRule())
			group.RateMultiplier, group.ProfitControlEnabled = 0.5, true
			account := inputRoutingAccount(2, 20)
			rate := 0.8
			account.RateMultiplier = &rate
			repo := inputRoutingAccountRepo{stubOpenAIAccountRepo{accounts: []Account{account}}}
			groups := inputRoutingGroupRepo{groups: map[int64]*Group{10: group, 20: {ID: 20, Platform: PlatformOpenAI, Status: StatusActive, RateMultiplier: 10}}}
			ctx := context.WithValue(context.Background(), ctxkey.Group, group)
			ctx, _ = WithGatewayTokenRequestPricing(ctx)
			ctx = WithGroupInputRoutingRequest(ctx, group, 50, false)
			var err error
			if schedulerName == "gateway" {
				svc := &GatewayService{accountRepo: repo, groupRepo: groups}
				_, err = svc.SelectAccountForModelWithExclusions(ctx, &group.ID, "", "gpt-5", nil)
			} else {
				svc := &OpenAIGatewayService{accountRepo: repo, cache: &schedulerTestGatewayCache{}}
				_, err = svc.SelectAccountWithLoadAwareness(ctx, &group.ID, "", "gpt-5", nil)
			}
			require.ErrorIs(t, err, ErrNoAvailableAccounts, "target groups cannot replace source pricing and profit requirements")
		})
	}
}

func TestGroupInputRoutingPlatformProbeDoesNotCountOtherPlatform(t *testing.T) {
	group := inputRoutingGroup(inputRoutingRule())
	repo := inputRoutingAccountRepo{stubOpenAIAccountRepo{accounts: []Account{inputRoutingAccount(2, 20)}}}
	svc := &GatewayService{accountRepo: repo}
	ctx := WithGroupInputRoutingRequest(context.Background(), group, 50, false)
	accounts, _, err := svc.listSchedulableAccounts(ctx, &group.ID, PlatformAntigravity, true)
	require.NoError(t, err)
	require.Empty(t, accounts, "single-platform retry probes must not count accounts from another platform")
}

type inputRoutingFailingGroupAccounts struct{ inputRoutingAccountRepo }

func (r inputRoutingFailingGroupAccounts) ListSchedulableByGroupIDAndPlatform(ctx context.Context, groupID int64, platform string) ([]Account, error) {
	if groupID == 20 {
		return nil, errors.New("target group unavailable")
	}
	return r.inputRoutingAccountRepo.ListSchedulableByGroupIDAndPlatform(ctx, groupID, platform)
}

func TestGroupInputRoutingUnavailableGroupDoesNotDiscardHealthyTargets(t *testing.T) {
	for _, schedulerName := range []string{"gateway", "openai"} {
		for _, tc := range []struct {
			name           string
			direct, groups []int64
			fallback       bool
			want           int64
		}{
			{"healthy direct", []int64{2}, []int64{20}, false, 2},
			{"healthy other group", nil, []int64{20, 30}, false, 4},
			{"no implicit fallback", nil, []int64{20}, false, 0},
			{"explicit fallback", nil, []int64{20}, true, 1},
		} {
			t.Run(schedulerName+"/"+tc.name, func(t *testing.T) {
				rule := inputRoutingRule()
				rule.AccountIDs, rule.GroupIDs, rule.FallbackToOriginal = tc.direct, tc.groups, tc.fallback
				group := inputRoutingGroup(rule)
				repo := inputRoutingFailingGroupAccounts{inputRoutingAccountRepo{stubOpenAIAccountRepo{accounts: []Account{inputRoutingAccount(1, 10), inputRoutingAccount(2, 40), inputRoutingAccount(3, 20), inputRoutingAccount(4, 30)}}}}
				groups := inputRoutingGroupRepo{groups: map[int64]*Group{10: group, 20: {ID: 20, Platform: PlatformOpenAI, Status: StatusActive}, 30: {ID: 30, Platform: PlatformOpenAI, Status: StatusActive}}}
				ctx := WithGroupInputRoutingRequest(context.WithValue(context.Background(), ctxkey.Group, group), group, 50, false)
				var selected *Account
				var err error
				if schedulerName == "gateway" {
					svc := &GatewayService{accountRepo: repo, groupRepo: groups}
					selected, err = svc.SelectAccountForModelWithExclusions(ctx, &group.ID, "", "gpt-5", nil)
				} else {
					svc := &OpenAIGatewayService{accountRepo: repo, schedulerSnapshot: &SchedulerSnapshotService{accountRepo: repo, groupRepo: groups}}
					var selection *AccountSelectionResult
					selection, _, err = newDefaultOpenAIAccountScheduler(svc, newOpenAIAccountRuntimeStats()).Select(ctx, OpenAIAccountScheduleRequest{GroupID: &group.ID, Platform: PlatformOpenAI, RequestedModel: "gpt-5"})
					if selection != nil {
						selected = selection.Account
						if selection.ReleaseFunc != nil {
							selection.ReleaseFunc()
						}
					}
				}
				if tc.want == 0 {
					require.ErrorIs(t, err, ErrNoAvailableAccounts)
					require.Nil(t, selected)
				} else {
					require.NoError(t, err)
					require.Equal(t, tc.want, selected.ID)
				}
			})
		}
	}
}

func TestGroupInputRoutingTerminalAdmissionRechecksWithoutProfitGate(t *testing.T) {
	for _, schedulerName := range []string{"gateway", "openai"} {
		for _, change := range []string{"membership", "target inactive", "target privacy", "target model", "account model", "account model quality", "unchanged", "fallback original", "fallback platform changed"} {
			t.Run(schedulerName+"/"+change, func(t *testing.T) {
				rule := inputRoutingRule()
				rule.AccountIDs, rule.GroupIDs = nil, []int64{20}
				if change == "fallback original" || change == "fallback platform changed" {
					rule.GroupIDs = []int64{999}
					rule.FallbackToOriginal = true
				}
				group := inputRoutingGroup(rule)
				target := &Group{ID: 20, Platform: PlatformOpenAI, Status: StatusActive}
				repo := inputRoutingAccountRepo{stubOpenAIAccountRepo{accounts: []Account{inputRoutingAccount(1, 10), inputRoutingAccount(2, 20)}}}
				groups := inputRoutingGroupRepo{groups: map[int64]*Group{10: group, 20: target}}
				snapshot := &SchedulerSnapshotService{accountRepo: repo, groupRepo: groups}
				cfg := &config.Config{}
				cfg.Gateway.Scheduling.LoadBatchEnabled = true
				concurrency := NewConcurrencyService(schedulerTestConcurrencyCache{acquireResults: map[int64]bool{1: false, 2: false}})
				gateway := &GatewayService{accountRepo: repo, groupRepo: groups, schedulerSnapshot: snapshot, cfg: cfg, concurrencyService: concurrency}
				openai := &OpenAIGatewayService{accountRepo: repo, schedulerSnapshot: snapshot, cfg: cfg, concurrencyService: concurrency}
				ctx := WithGroupInputRoutingRequest(context.WithValue(context.Background(), ctxkey.Group, group), group, 50, false)
				var selection *AccountSelectionResult
				var err error
				if schedulerName == "gateway" {
					selection, err = gateway.SelectAccountWithLoadAwareness(ctx, &group.ID, "", "gpt-5", nil, "", 0)
				} else {
					selection, _, err = newDefaultOpenAIAccountScheduler(openai, newOpenAIAccountRuntimeStats()).Select(ctx, OpenAIAccountScheduleRequest{GroupID: &group.ID, Platform: PlatformOpenAI, RequestedModel: "gpt-5"})
				}
				require.NoError(t, err)
				require.NotNil(t, selection.WaitPlan)
				require.False(t, selection.ProfitGateActive())
				switch change {
				case "membership":
					repo.accounts[1].GroupIDs = []int64{30}
					repo.accounts[1].AccountGroups = []AccountGroup{{GroupID: 30}}
				case "target inactive":
					target.Status = "disabled"
				case "target privacy":
					target.RequirePrivacySet = true
				case "target model":
					target.ModelAllowlist = GroupModelAllowlist{Enabled: true, Models: []string{"gpt-denied"}}
				case "account model":
					repo.accounts[1].Credentials = map[string]any{"model_mapping": map[string]any{"other-model": "other-model"}}
				case "account model quality":
					repo.accounts[1].Extra = map[string]any{"account_quality_gates": map[string]any{"17": map[string]any{"policy_id": float64(9), "policy_version": float64(1), "scope": "model", "model": "gpt-5", "blocked": true}}}
				case "fallback platform changed":
					repo.accounts[0].Platform = PlatformAnthropic
				}
				admissionCtx := ContextWithSelectionProfitGate(ctx, selection)
				var latest *Account
				var vetoed bool
				var reason string
				if schedulerName == "gateway" {
					latest, vetoed, reason = gateway.GatewayProfitControlVetoLatest(admissionCtx, selection.Account)
				} else {
					latest, vetoed, reason = openai.ProfitControlVetoLatest(admissionCtx, selection.Account)
				}
				if change == "unchanged" || change == "fallback original" {
					require.False(t, vetoed, reason)
					require.Equal(t, selection.Account.ID, latest.ID)
				} else {
					require.True(t, vetoed, "route changed while waiting must be rejected")
					require.Contains(t, reason, "input_routing")
				}
			})
		}
	}
}

func TestGroupInputRoutingCanceledRequestNeverFallsBack(t *testing.T) {
	rule := inputRoutingRule()
	rule.FallbackToOriginal = true
	group := inputRoutingGroup(rule)
	repo := inputRoutingAccountRepo{stubOpenAIAccountRepo{accounts: []Account{inputRoutingAccount(1, 10), inputRoutingAccount(2, 20)}}}
	groups := inputRoutingGroupRepo{groups: map[int64]*Group{10: group}}
	ctx, cancel := context.WithCancel(context.Background())
	ctx = WithGroupInputRoutingRequest(ctx, group, 50, false)
	cancel()
	svc := &GatewayService{accountRepo: repo, groupRepo: groups}
	selected, err := svc.SelectAccountForModelWithExclusions(ctx, &group.ID, "", "", nil)
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, selected)
}

func TestGroupInputRoutingCrossPlatformTargetsUseRequestProtocol(t *testing.T) {
	for _, loadAware := range []bool{false, true} {
		for _, tc := range []struct {
			protocol, source, target string
			targetType               string
			allowed                  bool
		}{
			{"messages", PlatformAnthropic, PlatformOpenAI, AccountTypeOAuth, true},
			{"messages", PlatformOpenAI, PlatformAnthropic, AccountTypeAPIKey, true},
			{"messages", PlatformComposite, PlatformOpenAI, AccountTypeOAuth, true},
			{"messages", PlatformGemini, PlatformGrok, AccountTypeAPIKey, true},
			{"messages", PlatformOpenAI, PlatformGemini, AccountTypeAPIKey, true},
			{"chat", PlatformOpenAI, PlatformGemini, AccountTypeAPIKey, true},
			{"chat", PlatformGemini, PlatformAnthropic, AccountTypeAPIKey, true},
			{"responses", PlatformAnthropic, PlatformOpenAI, AccountTypeOAuth, true},
			{"responses", PlatformOpenAI, PlatformGemini, AccountTypeOAuth, false},
			{"responses", PlatformGemini, PlatformAnthropic, AccountTypeAPIKey, true},
			{"gemini", PlatformGemini, PlatformAntigravity, AccountTypeOAuth, true},
			{"gemini", PlatformGemini, PlatformOpenAI, AccountTypeOAuth, false},
			{"messages", PlatformAnthropic, PlatformTypeSafe, AccountTypeAPIKey, false},
		} {
			t.Run(tc.protocol+"/"+tc.source+"/"+tc.target+map[bool]string{false: "/legacy", true: "/load"}[loadAware], func(t *testing.T) {
				for _, groupTarget := range []bool{false, true} {
					rule := inputRoutingRule()
					if groupTarget {
						rule.AccountIDs = nil
						rule.GroupIDs = []int64{20}
					}
					group := inputRoutingGroup(rule)
					group.Platform = tc.source
					group.AllowMessagesDispatch = true
					account := inputRoutingAccount(2, 20)
					account.Platform = tc.target
					account.Type = tc.targetType
					account.Credentials = map[string]any{"model_mapping": map[string]any{"request-model": "upstream-model"}}
					repo := inputRoutingAccountRepo{stubOpenAIAccountRepo{accounts: []Account{account}}}
					groups := inputRoutingGroupRepo{groups: map[int64]*Group{10: group, 20: {ID: 20, Platform: tc.target, Status: StatusActive}}}
					cfg := &config.Config{}
					cfg.Gateway.Scheduling.LoadBatchEnabled = true
					svc := &GatewayService{accountRepo: repo, groupRepo: groups, cfg: cfg, concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{})}
					svc.SetInputRoutingOpenAIGateway(&OpenAIGatewayService{accountRepo: repo})
					ctx := WithGroupInputRoutingProtocol(context.WithValue(context.Background(), ctxkey.Group, group), tc.protocol)
					ctx = WithGroupInputRoutingRequest(ctx, group, 50, false)
					var selected *Account
					var err error
					if loadAware {
						var result *AccountSelectionResult
						result, err = svc.SelectAccountWithLoadAwareness(ctx, &group.ID, "", "request-model", nil, "", 0)
						if result != nil {
							selected = result.Account
							if result.ReleaseFunc != nil {
								result.ReleaseFunc()
							}
						}
					} else {
						selected, err = svc.SelectAccountForModelWithExclusions(ctx, &group.ID, "", "request-model", nil)
					}
					if tc.allowed {
						require.NoError(t, err)
						require.Equal(t, int64(2), selected.ID)
						require.Equal(t, tc.target, selected.Platform)
					} else {
						require.ErrorIs(t, err, ErrNoAvailableAccounts)
						require.Nil(t, selected)
					}
				}
			})
		}
	}
}

func TestGroupInputRoutingCrossPlatformKeepsNativeModelQuotaAndRuntimeGates(t *testing.T) {
	for _, blocked := range []bool{false, true} {
		group := inputRoutingGroup(inputRoutingRule())
		group.Platform = PlatformAnthropic
		account := inputRoutingAccount(2, 20)
		account.Type = AccountTypeOAuth
		account.Credentials = map[string]any{"model_mapping": map[string]any{"alias": "gpt-5"}}
		if blocked {
			account.Extra = map[string]any{"account_quality_gates": map[string]any{"17": map[string]any{"scope": "model", "model": "gpt-5", "blocked": true}}}
		}
		repo := inputRoutingAccountRepo{stubOpenAIAccountRepo{accounts: []Account{account}}}
		svc := &GatewayService{accountRepo: repo, groupRepo: inputRoutingGroupRepo{groups: map[int64]*Group{10: group}}}
		svc.SetInputRoutingOpenAIGateway(&OpenAIGatewayService{accountRepo: repo})
		ctx := WithGroupInputRoutingProtocol(context.WithValue(context.Background(), ctxkey.Group, group), "messages")
		ctx = WithGroupInputRoutingRequest(ctx, group, 50, false)
		selected, err := svc.SelectAccountForModelWithExclusions(ctx, &group.ID, "", "alias", nil)
		if blocked {
			require.ErrorIs(t, err, ErrNoAvailableAccounts)
			require.Nil(t, selected)
		} else {
			require.NoError(t, err)
			require.Equal(t, int64(2), selected.ID)
		}
	}
}

func TestGroupInputRoutingCrossPlatformHonorsOpenAIQuotaAndShadowParent(t *testing.T) {
	for _, reason := range []string{"quota", "shadow parent", "missing native gateway"} {
		t.Run(reason, func(t *testing.T) {
			group := inputRoutingGroup(inputRoutingRule())
			group.Platform = PlatformAnthropic
			account := inputRoutingAccount(2, 20)
			account.Type = AccountTypeOAuth
			parent := inputRoutingAccount(9, 20)
			parent.Schedulable = false
			if reason == "quota" {
				account.Extra = map[string]any{"codex_7d_used_percent": 95.0, "codex_7d_reset_at": time.Now().Add(24 * time.Hour).Format(time.RFC3339), "codex_usage_updated_at": time.Now().Add(-time.Minute).Format(time.RFC3339)}
			}
			if reason == "shadow parent" {
				account.ParentAccountID = &parent.ID
			}
			repo := inputRoutingAccountRepo{stubOpenAIAccountRepo{accounts: []Account{account, parent}}}
			svc := &GatewayService{accountRepo: repo, groupRepo: inputRoutingGroupRepo{groups: map[int64]*Group{10: group}}, cache: &schedulerTestGatewayCache{sessionBindings: map[string]int64{"sticky": 2}}}
			if reason != "missing native gateway" {
				svc.SetInputRoutingOpenAIGateway(&OpenAIGatewayService{accountRepo: repo})
			}
			ctx := withOpenAIQuotaAutoPauseSettings(context.WithValue(context.Background(), ctxkey.Group, group), OpsOpenAIAccountQuotaAutoPauseSettings{DefaultThreshold7d: 0.9})
			ctx = WithGroupInputRoutingRequest(WithGroupInputRoutingProtocol(ctx, "messages"), group, 50, false)
			selected, err := svc.SelectAccountForModelWithExclusions(ctx, &group.ID, "sticky", "gpt-5", nil)
			require.ErrorIs(t, err, ErrNoAvailableAccounts)
			require.Nil(t, selected)
		})
	}
}

func TestGroupInputRoutingCrossPlatformFallbackKeepsOriginalGroupIdentity(t *testing.T) {
	rule := inputRoutingRule()
	rule.FallbackToOriginal = true
	group := inputRoutingGroup(rule)
	group.Platform = PlatformAnthropic
	original := inputRoutingAccount(1, 10)
	original.Platform = PlatformAnthropic
	target := inputRoutingAccount(2, 20)
	target.Schedulable = false
	repo := inputRoutingAccountRepo{stubOpenAIAccountRepo{accounts: []Account{original, target}}}
	svc := &GatewayService{accountRepo: repo, groupRepo: inputRoutingGroupRepo{groups: map[int64]*Group{10: group}}}
	svc.SetInputRoutingOpenAIGateway(&OpenAIGatewayService{accountRepo: repo})
	ctx := WithGroupInputRoutingRequest(WithGroupInputRoutingProtocol(context.WithValue(context.Background(), ctxkey.Group, group), "messages"), group, 50, false)
	selected, err := svc.SelectAccountForModelWithExclusions(ctx, &group.ID, "", "", nil)
	require.NoError(t, err)
	require.Equal(t, int64(1), selected.ID)
	require.Same(t, group, ctx.Value(ctxkey.Group))
}

func TestGroupInputRoutingCrossPlatformMessagesPreservesSourceDispatchPermission(t *testing.T) {
	for _, source := range []string{PlatformOpenAI, PlatformComposite, PlatformAnthropic} {
		for _, allowed := range []bool{false, true} {
			for _, target := range []string{PlatformOpenAI, PlatformGrok, PlatformDeepseek} {
				t.Run(source+"/"+target+map[bool]string{false: "/disabled", true: "/enabled"}[allowed], func(t *testing.T) {
					rule := inputRoutingRule()
					rule.AccountIDs = []int64{2, 3}
					group := inputRoutingGroup(rule)
					group.Platform = source
					group.AllowMessagesDispatch = allowed
					preferred := inputRoutingAccount(2, 20)
					preferred.Platform = target
					fallback := inputRoutingAccount(3, 30)
					fallback.Platform = PlatformAnthropic
					repo := inputRoutingAccountRepo{stubOpenAIAccountRepo{accounts: []Account{preferred, fallback}}}
					svc := &GatewayService{accountRepo: repo, groupRepo: inputRoutingGroupRepo{groups: map[int64]*Group{10: group}}}
					svc.SetInputRoutingOpenAIGateway(&OpenAIGatewayService{accountRepo: repo})
					ctx := WithGroupInputRoutingRequest(WithGroupInputRoutingProtocol(context.WithValue(context.Background(), ctxkey.Group, group), "messages"), group, 50, false)
					selected, err := svc.SelectAccountForModelWithExclusions(ctx, &group.ID, "", "", nil)
					require.NoError(t, err)
					want := int64(2)
					if !allowed && target == PlatformOpenAI && (source == PlatformOpenAI || source == PlatformComposite) {
						want = 3
					}
					require.Equal(t, want, selected.ID, "only OpenAI targets require the original OpenAI/composite Messages dispatch permission")
				})
			}
		}
	}
}
