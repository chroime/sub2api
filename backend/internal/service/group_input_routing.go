package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
)

type groupInputRoutingRequestKey struct{}
type groupInputRoutingPassKey struct{}
type groupInputRoutingProtocolKey struct{}

// WithGroupInputRoutingProtocol identifies the downstream HTTP protocol so
// targets are filtered by forwarding support rather than their source platform.
func WithGroupInputRoutingProtocol(ctx context.Context, protocol string) context.Context {
	return context.WithValue(ctx, groupInputRoutingProtocolKey{}, strings.ToLower(strings.TrimSpace(protocol)))
}

// SetInputRoutingOpenAIGateway connects the native OpenAI account eligibility
// checks used by the shared HTTP routing pool. Call during handler construction.
func (s *GatewayService) SetInputRoutingOpenAIGateway(gateway *OpenAIGatewayService) {
	if s != nil {
		s.inputRoutingOpenAIGateway = gateway
	}
}

func groupInputRoutingProtocol(ctx context.Context) string {
	protocol, _ := ctx.Value(groupInputRoutingProtocolKey{}).(string)
	return protocol
}

// GroupInputRoutingDecision describes the matched rule without changing the
// authenticated group, its prices, or any account's actual group memberships.
type GroupInputRoutingDecision struct {
	SourceGroupID        int64
	RuleID               string
	RuleName             string
	EstimatedInputTokens int
	RequestType          string
	TargetAccountIDs     []int64
	TargetGroupIDs       []int64
	FallbackToOriginal   bool
}

type groupInputRoutingRequest struct {
	decision GroupInputRoutingDecision
	source   *Group
}

type groupInputRoutingPass struct {
	platform        string
	model           string
	requestedModel  string
	requireCompact  bool
	protocol        string
	validateAccount func(context.Context, *Account) bool
	accounts        []Account
	direct          map[int64]struct{}
	groups          map[int64]*Group
	rejected        map[int64]struct{}
	original        bool
}

type groupInputRoutingAccountChangedError struct{ accountID int64 }

func (e *groupInputRoutingAccountChangedError) Error() string {
	return fmt.Sprintf("input routing account %d membership changed: %v", e.accountID, ErrNoAvailableAccounts)
}

func (e *groupInputRoutingAccountChangedError) Unwrap() error { return ErrNoAvailableAccounts }

// WithGroupInputRoutingRequest selects the first request-type/input-condition match.
// Unmatched or unknown input retains the source group's ordinary scheduling.
// Preparation is idempotent: a retry or another group's rules cannot replace
// the source request's scope.
func WithGroupInputRoutingRequest(ctx context.Context, group *Group, estimatedInputTokens int, stream bool) context.Context {
	if groupInputRoutingRequestFromContext(ctx) != nil || group == nil || !group.InputRouting.Enabled || estimatedInputTokens < 0 {
		return ctx
	}
	requestType := "sync"
	if stream {
		requestType = "stream"
	}
	for _, rule := range group.InputRouting.Rules {
		if !rule.Enabled || (rule.RequestType != "all" && rule.RequestType != requestType) {
			continue
		}
		switch rule.InputTokensMode {
		case "all":
			// All eligible input lengths match without consulting dormant bounds.
		case "", "range":
			if rule.MinInputTokens < 0 || rule.MaxInputTokens < rule.MinInputTokens ||
				estimatedInputTokens < rule.MinInputTokens || estimatedInputTokens > rule.MaxInputTokens {
				continue
			}
		default:
			continue
		}
		source := *group
		request := &groupInputRoutingRequest{source: &source, decision: GroupInputRoutingDecision{
			SourceGroupID: group.ID, RuleID: rule.ID, RuleName: rule.Name,
			EstimatedInputTokens: estimatedInputTokens, RequestType: requestType,
			TargetAccountIDs:   append([]int64(nil), rule.AccountIDs...),
			TargetGroupIDs:     append([]int64(nil), rule.GroupIDs...),
			FallbackToOriginal: rule.FallbackToOriginal,
		}}
		return context.WithValue(ctx, groupInputRoutingRequestKey{}, request)
	}
	return ctx
}

func groupInputRoutingRequestFromContext(ctx context.Context) *groupInputRoutingRequest {
	request, _ := ctx.Value(groupInputRoutingRequestKey{}).(*groupInputRoutingRequest)
	return request
}

// GroupInputRoutingDecisionFromContext returns a copy suitable for structured
// request diagnostics. It contains no request text or credentials.
func GroupInputRoutingDecisionFromContext(ctx context.Context) (GroupInputRoutingDecision, bool) {
	request := groupInputRoutingRequestFromContext(ctx)
	if request == nil {
		return GroupInputRoutingDecision{}, false
	}
	decision := request.decision
	decision.TargetAccountIDs = append([]int64(nil), decision.TargetAccountIDs...)
	decision.TargetGroupIDs = append([]int64(nil), decision.TargetGroupIDs...)
	return decision, true
}

func groupInputRoutingPassFromContext(ctx context.Context) *groupInputRoutingPass {
	pass, _ := ctx.Value(groupInputRoutingPassKey{}).(*groupInputRoutingPass)
	return pass
}

func groupInputRoutingGroupMatches(ctx context.Context, groupID *int64) bool {
	request := groupInputRoutingRequestFromContext(ctx)
	return request == nil || (groupID != nil && *groupID == request.decision.SourceGroupID)
}

func groupInputRoutingGroupAccountAllowed(group *Group, account *Account) bool {
	return account != nil && group != nil &&
		(!group.RequirePrivacySet || account.IsPrivacySet()) &&
		(!group.RequireOAuthOnly || account.Type != AccountTypeAPIKey)
}

// groupInputRoutingAccountAllowed reports whether this request has an active
// scope and whether a freshly loaded account still belongs to that scope.
func groupInputRoutingAccountAllowed(ctx context.Context, groupID *int64, account *Account) (bool, bool) {
	request := groupInputRoutingRequestFromContext(ctx)
	if request == nil {
		return true, false
	}
	if !groupInputRoutingGroupMatches(ctx, groupID) || !groupInputRoutingGroupAccountAllowed(request.source, account) {
		return false, true
	}
	pass := groupInputRoutingPassFromContext(ctx)
	if pass == nil {
		return false, true
	}
	if pass.protocol == "messages" && account.Platform == PlatformOpenAI &&
		(request.source.Platform == PlatformOpenAI || request.source.Platform == PlatformComposite) && !request.source.AllowMessagesDispatch {
		return false, true
	}
	if _, rejected := pass.rejected[account.ID]; rejected {
		return false, true
	}
	if pass.original {
		// The original pool retains its existing mixed-platform policy; its
		// scheduler still enforces platform eligibility after this membership gate.
		forcePlatform, _ := ctx.Value(ctxkey.ForcePlatform).(string)
		platformAllowed := account.Platform == pass.platform || (forcePlatform == "" &&
			(pass.platform == PlatformAnthropic || pass.platform == PlatformGemini) && account.Platform == PlatformAntigravity && account.IsMixedSchedulingEnabled())
		if pass.protocol != "" {
			platformAllowed = groupInputRoutingProtocolSupportsAccount(pass.protocol, account)
		}
		return platformAllowed && openAIStickyAccountMatchesGroup(account, groupID), true
	}
	if (pass.protocol == "" && account.Platform != pass.platform) || (pass.protocol != "" && !groupInputRoutingProtocolSupportsAccount(pass.protocol, account)) {
		return false, true
	}
	if _, ok := pass.direct[account.ID]; ok {
		return true, true
	}
	for id, group := range pass.groups {
		if openAIStickyAccountMatchesGroup(account, &id) && groupInputRoutingGroupAccountAllowed(group, account) {
			return true, true
		}
	}
	return false, true
}

func prepareGroupInputRouting(
	ctx context.Context, groupID *int64, platform, model string,
	getGroup func(context.Context, int64) (*Group, error),
	getAccount func(context.Context, int64) (*Account, error),
	listGroup func(context.Context, *int64, string) ([]Account, error),
) (context.Context, error) {
	request := groupInputRoutingRequestFromContext(ctx)
	if request == nil {
		return ctx, nil
	}
	if !groupInputRoutingGroupMatches(ctx, groupID) {
		return ctx, fmt.Errorf("%w: input routing source group changed", ErrNoAvailableAccounts)
	}
	if pass := groupInputRoutingPassFromContext(ctx); pass != nil {
		if pass.platform != platform && pass.protocol == "" {
			return ctx, fmt.Errorf("%w: input routing platform changed", ErrNoAvailableAccounts)
		}
		return ctx, nil
	}
	pass := &groupInputRoutingPass{platform: platform, requestedModel: model, protocol: groupInputRoutingProtocol(ctx), direct: make(map[int64]struct{}), groups: make(map[int64]*Group)}
	seen := make(map[int64]struct{})
	add := func(account *Account) {
		if account == nil || !groupInputRoutingGroupAccountAllowed(request.source, account) ||
			(pass.protocol == "" && account.Platform != platform) || (pass.protocol != "" && !groupInputRoutingProtocolSupportsAccount(pass.protocol, account)) {
			return
		}
		if _, exists := seen[account.ID]; exists {
			return
		}
		seen[account.ID] = struct{}{}
		pass.accounts = append(pass.accounts, *account)
	}
	for _, id := range request.decision.TargetAccountIDs {
		if id <= 0 {
			continue
		}
		account, err := getAccount(ctx, id)
		if err != nil {
			if ctx.Err() != nil {
				return ctx, ctx.Err()
			}
			continue
		}
		if account != nil {
			pass.direct[id] = struct{}{}
			add(account)
		}
	}
	if publicModel, ok := RequestedPublicModelFromContext(ctx); ok {
		model = publicModel
	}
	pass.model = model
	for _, id := range request.decision.TargetGroupIDs {
		if id <= 0 || id == request.decision.SourceGroupID || getGroup == nil {
			continue
		}
		if _, exists := pass.groups[id]; exists {
			continue
		}
		group, err := getGroup(ctx, id)
		if err != nil {
			if ctx.Err() != nil {
				return ctx, ctx.Err()
			}
			continue
		}
		if !groupInputRoutingTargetGroupAllowed(ctx, request, group, platform, model) {
			continue
		}
		accounts, err := listGroup(ctx, &id, platform)
		if err != nil {
			if ctx.Err() != nil {
				return ctx, ctx.Err()
			}
			slog.Warn("group_input_routing_target_group_unavailable", "source_group_id", request.decision.SourceGroupID,
				"rule_id", request.decision.RuleID, "target_group_id", id)
			continue
		}
		pass.groups[id] = group
		for i := range accounts {
			account := &accounts[i]
			// A stale cache bucket (or simple mode) cannot import unrelated members.
			if openAIStickyAccountMatchesGroup(account, &id) && groupInputRoutingGroupAccountAllowed(group, account) {
				add(account)
			}
		}
	}
	return context.WithValue(ctx, groupInputRoutingPassKey{}, pass), nil
}

// Only the outer selection call owns the original-group fallback. Nested
// legacy, sticky, image-size and retry paths must first exhaust the target pool.
func selectWithGroupInputRouting[T any](ctx context.Context, groupID *int64, prepare func(context.Context) (context.Context, error), selectAccount func(context.Context) (T, error)) (T, error) {
	var zero T
	request := groupInputRoutingRequestFromContext(ctx)
	if request == nil {
		return selectAccount(ctx)
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if !groupInputRoutingGroupMatches(ctx, groupID) {
		return zero, fmt.Errorf("%w: input routing source group changed", ErrNoAvailableAccounts)
	}
	if groupInputRoutingPassFromContext(ctx) != nil {
		return selectAccount(ctx)
	}
	prepared, err := prepare(ctx)
	if err != nil {
		return zero, err
	}
	result, prepared, err := selectGroupInputRoutingPass(prepared, selectAccount)
	if err == nil {
		logGroupInputRoutingSelection(prepared, result)
		return result, nil
	}
	if !request.decision.FallbackToOriginal || (!errors.Is(err, ErrNoAvailableAccounts) && !errors.Is(err, ErrNoAvailableCompactAccounts)) {
		return result, err
	}
	pass := *groupInputRoutingPassFromContext(prepared)
	pass.original = true
	pass.accounts = nil
	slog.Info("group_input_routing_original_fallback", "source_group_id", request.decision.SourceGroupID, "rule_id", request.decision.RuleID)
	fallbackCtx := context.WithValue(prepared, groupInputRoutingPassKey{}, &pass)
	result, fallbackCtx, err = selectGroupInputRoutingPass(fallbackCtx, selectAccount)
	if err == nil {
		logGroupInputRoutingSelection(fallbackCtx, result)
	}
	return result, err
}

func groupInputRoutingTargetGroupAllowed(ctx context.Context, request *groupInputRoutingRequest, group *Group, platform, model string) bool {
	return group != nil && group.IsActive() && group.ModelAllowlist.Allows(model) &&
		(!group.ClaudeCodeOnly || IsClaudeCodeClient(ctx))
}

func groupInputRoutingProtocolSupportsAccount(protocol string, account *Account) bool {
	if account == nil || account.Platform == PlatformTypeSafe {
		return false
	}
	switch protocol {
	case "messages", "chat":
		return account.Platform == PlatformAnthropic || account.Platform == PlatformGemini || account.Platform == PlatformAntigravity || account.IsOpenAICompatible()
	case "responses":
		return account.Platform == PlatformAnthropic || account.Platform == PlatformAntigravity || account.IsOpenAICompatible()
	case "gemini":
		return account.Platform == PlatformGemini || account.Platform == PlatformAntigravity
	default:
		return false
	}
}

func (s *GatewayService) isAccountAllowedForInputRoutingPlatform(ctx context.Context, account *Account, platform string, useMixed bool) bool {
	if pass := groupInputRoutingPassFromContext(ctx); pass != nil && pass.protocol != "" {
		return groupInputRoutingProtocolSupportsAccount(pass.protocol, account)
	}
	return s.isAccountAllowedForPlatform(account, platform, useMixed)
}

func (s *GatewayService) inputRoutingNativeAccountEligible(ctx context.Context, account *Account, model string) bool {
	pass := groupInputRoutingPassFromContext(ctx)
	if pass == nil || pass.protocol == "" || account == nil || !account.IsOpenAICompatible() {
		return true
	}
	openai := s.inputRoutingOpenAIGateway
	if openai == nil {
		return false
	}
	ctx = openai.withOpenAIQuotaAutoPauseContext(ctx)
	if !isOpenAICompatibleAccountEligibleForRequestBeforeProfit(ctx, account, account.Platform, model, false, OpenAIEndpointCapabilityChatCompletions) {
		return false
	}
	request := groupInputRoutingRequestFromContext(ctx)
	scheduler := &defaultOpenAIAccountScheduler{service: openai}
	if !scheduler.isAccountRequestCompatible(ctx, account, OpenAIAccountScheduleRequest{GroupID: &request.decision.SourceGroupID, Platform: account.Platform, RequestedModel: model, RequiredCapability: OpenAIEndpointCapabilityChatCompletions}) {
		return false
	}
	if openai.isOpenAIAccountBlockedBySchedulingThreshold(ctx, account) {
		return false
	}
	if account.IsGrok() {
		if len(openai.filterGrokFreeQuotaAccountsForOpenAI(ctx, []Account{*account})) == 0 {
			return false
		}
		now := time.Now()
		upstream := canonicalOpenAIAccountSchedulingModel(account, model)
		if isGrokTeamModelRateLimited(account, upstream, now) || isGrokModelQuotaBlocked(account.ID, upstream, now) {
			return false
		}
	}
	return true
}

// A final hydration failure removes only that account from the current pass.
// Each retry makes progress, so stale candidates cannot cause an unbounded loop
// or trigger original-pool fallback while another target remains usable.
func selectGroupInputRoutingPass[T any](ctx context.Context, selectAccount func(context.Context) (T, error)) (T, context.Context, error) {
	for {
		if err := ctx.Err(); err != nil {
			var zero T
			return zero, ctx, err
		}
		result, err := selectAccount(ctx)
		var changed *groupInputRoutingAccountChangedError
		if !errors.As(err, &changed) {
			return result, ctx, err
		}
		pass := *groupInputRoutingPassFromContext(ctx)
		if _, rejected := pass.rejected[changed.accountID]; rejected {
			return result, ctx, err
		}
		rejected := make(map[int64]struct{}, len(pass.rejected)+1)
		for id := range pass.rejected {
			rejected[id] = struct{}{}
		}
		rejected[changed.accountID] = struct{}{}
		pass.rejected = rejected
		accounts := make([]Account, 0, len(pass.accounts))
		for _, account := range pass.accounts {
			if account.ID != changed.accountID {
				accounts = append(accounts, account)
			}
		}
		pass.accounts = accounts
		ctx = context.WithValue(ctx, groupInputRoutingPassKey{}, &pass)
	}
}

func logGroupInputRoutingSelection(ctx context.Context, result any) {
	request := groupInputRoutingRequestFromContext(ctx)
	pass := groupInputRoutingPassFromContext(ctx)
	if request == nil || pass == nil {
		return
	}
	var account *Account
	switch result := result.(type) {
	case *Account:
		account = result
	case *AccountSelectionResult:
		if result != nil {
			account = result.Account
		}
	case openAIInputRoutingStickySelection:
		account = result.account
	case openAIInputRoutingSelection:
		if result.selection != nil {
			account = result.selection.Account
		}
	}
	if account == nil {
		return
	}
	targetGroupIDs := make([]int64, 0, len(pass.groups))
	seen := make(map[int64]struct{})
	if !pass.original {
		for _, id := range request.decision.TargetGroupIDs {
			if _, duplicate := seen[id]; duplicate {
				continue
			}
			seen[id] = struct{}{}
			if group, ok := pass.groups[id]; ok && openAIStickyAccountMatchesGroup(account, &id) && groupInputRoutingGroupAccountAllowed(group, account) {
				targetGroupIDs = append(targetGroupIDs, id)
			}
		}
	}
	slog.Info("group_input_routing_selected", "source_group_id", request.decision.SourceGroupID, "rule_id", request.decision.RuleID,
		"target_group_ids", targetGroupIDs, "account_id", account.ID, "fallback_to_original", pass.original)
}

func (s *GatewayService) prepareInputRouting(ctx context.Context, groupID *int64, requestedModel string) (context.Context, error) {
	request := groupInputRoutingRequestFromContext(ctx)
	if request == nil {
		return ctx, nil
	}
	if groupInputRoutingProtocol(ctx) != "" {
		return s.prepareInputRoutingForPlatform(ctx, groupID, request.source.Platform, requestedModel)
	}
	platform, _, err := s.resolvePlatform(ctx, groupID, request.source, requestedModel)
	if err != nil {
		return ctx, err
	}
	return s.prepareInputRoutingForPlatform(ctx, groupID, platform, requestedModel)
}

func (s *GatewayService) prepareInputRoutingForPlatform(ctx context.Context, groupID *int64, platform, requestedModel string) (context.Context, error) {
	if groupInputRoutingProtocol(ctx) != "" && s.inputRoutingOpenAIGateway != nil {
		ctx = s.inputRoutingOpenAIGateway.withOpenAIQuotaAutoPauseContext(ctx)
	}
	prepared, err := prepareGroupInputRouting(ctx, groupID, platform, requestedModel, s.resolveGroupByID, s.getSchedulableAccount,
		func(ctx context.Context, id *int64, platform string) ([]Account, error) {
			if groupInputRoutingProtocol(ctx) != "" {
				accounts, err := s.accountRepo.ListSchedulableByGroupID(ctx, *id)
				if err != nil {
					return nil, err
				}
				return s.filterAccountsBySchedulingThreshold(ctx, accounts), nil
			}
			accounts, _, err := s.listSchedulableAccountsWithoutInputRouting(ctx, id, platform, true)
			return accounts, err
		})
	if err == nil && groupInputRoutingPassFromContext(ctx) == nil {
		if pass := groupInputRoutingPassFromContext(prepared); pass != nil && pass.protocol != "" {
			pass.validateAccount = func(ctx context.Context, account *Account) bool {
				model := requestedModel
				if currentPass := groupInputRoutingPassFromContext(ctx); currentPass != nil {
					model = currentPass.requestedModel
				}
				return s.inputRoutingNativeAccountEligible(ctx, account, model)
			}
		}
	}
	return prepared, err
}

func (s *OpenAIGatewayService) prepareInputRouting(ctx context.Context, groupID *int64, platform, requestedModel string, requireCompact ...bool) (context.Context, error) {
	var getGroup func(context.Context, int64) (*Group, error)
	if s.schedulerSnapshot != nil {
		getGroup = s.schedulerSnapshot.GetGroupByIDLite
	}
	prepared, err := prepareGroupInputRouting(ctx, groupID, NormalizeOpenAICompatiblePlatform(platform), requestedModel, getGroup, s.getSchedulableAccount, s.listSchedulableAccountsWithoutInputRouting)
	if err == nil && groupInputRoutingPassFromContext(ctx) == nil {
		if pass := groupInputRoutingPassFromContext(prepared); pass != nil {
			pass.requireCompact = len(requireCompact) > 0 && requireCompact[0]
		}
	}
	return prepared, err
}

func (s *GatewayService) SelectAccountForModelWithExclusions(ctx context.Context, groupID *int64, sessionHash, requestedModel string, excludedIDs map[int64]struct{}) (*Account, error) {
	return selectWithGroupInputRouting(ctx, groupID,
		func(ctx context.Context) (context.Context, error) {
			return s.prepareInputRouting(ctx, groupID, requestedModel)
		},
		func(ctx context.Context) (*Account, error) {
			return s.selectAccountForModelWithExclusionsOnce(ctx, groupID, sessionHash, requestedModel, excludedIDs)
		})
}

func (s *GatewayService) SelectAccountWithLoadAwareness(ctx context.Context, groupID *int64, sessionHash, requestedModel string, excludedIDs map[int64]struct{}, metadataUserID string, sub2apiUserID int64) (*AccountSelectionResult, error) {
	return selectWithGroupInputRouting(ctx, groupID,
		func(ctx context.Context) (context.Context, error) {
			return s.prepareInputRouting(ctx, groupID, requestedModel)
		},
		func(ctx context.Context) (*AccountSelectionResult, error) {
			return s.selectAccountWithLoadAwarenessOnce(ctx, groupID, sessionHash, requestedModel, excludedIDs, metadataUserID, sub2apiUserID)
		})
}

func filterGroupInputRoutingAccounts(ctx context.Context, groupID *int64, accounts []Account) []Account {
	filtered := make([]Account, 0, len(accounts))
	for _, account := range accounts {
		if allowed, _ := groupInputRoutingAccountAllowed(ctx, groupID, &account); allowed {
			filtered = append(filtered, account)
		}
	}
	return filtered
}

func (s *GatewayService) listSchedulableAccounts(ctx context.Context, groupID *int64, platform string, hasForcePlatform bool) ([]Account, bool, error) {
	if groupInputRoutingRequestFromContext(ctx) == nil {
		return s.listSchedulableAccountsWithoutInputRouting(ctx, groupID, platform, hasForcePlatform)
	}
	prepared, err := s.prepareInputRoutingForPlatform(ctx, groupID, platform, accountQualitySelectionModelFromInputRouting(ctx))
	if err != nil {
		return nil, false, err
	}
	pass := groupInputRoutingPassFromContext(prepared)
	if !pass.original {
		return append([]Account(nil), pass.accounts...), false, nil
	}
	var accounts []Account
	var mixed bool
	if pass.protocol != "" {
		accounts, err = s.accountRepo.ListSchedulableByGroupID(prepared, *groupID)
		accounts = s.filterAccountsBySchedulingThreshold(prepared, accounts)
	} else {
		accounts, mixed, err = s.listSchedulableAccountsWithoutInputRouting(prepared, groupID, platform, hasForcePlatform)
	}
	return filterGroupInputRoutingAccounts(prepared, groupID, accounts), mixed, err
}

func groupInputRoutingUsesTargetProtocolPool(ctx context.Context) bool {
	pass := groupInputRoutingPassFromContext(ctx)
	return pass != nil && pass.protocol != "" && !pass.original
}

func groupInputRoutingHasProtocolPool(ctx context.Context) bool {
	pass := groupInputRoutingPassFromContext(ctx)
	return pass != nil && pass.protocol != ""
}

func accountQualitySelectionModelFromInputRouting(ctx context.Context) string {
	model, _ := ctx.Value(accountQualitySelectionModelKey{}).(string)
	return model
}

func (s *OpenAIGatewayService) listSchedulableAccounts(ctx context.Context, groupID *int64, platform string) ([]Account, error) {
	if groupInputRoutingRequestFromContext(ctx) == nil {
		return s.listSchedulableAccountsWithoutInputRouting(ctx, groupID, platform)
	}
	prepared, err := s.prepareInputRouting(ctx, groupID, platform, accountQualitySelectionModelFromInputRouting(ctx))
	if err != nil {
		return nil, err
	}
	pass := groupInputRoutingPassFromContext(prepared)
	if !pass.original {
		return append([]Account(nil), pass.accounts...), nil
	}
	accounts, err := s.listSchedulableAccountsWithoutInputRouting(prepared, groupID, platform)
	return filterGroupInputRoutingAccounts(prepared, groupID, accounts), err
}

func (s *GatewayService) isAccountInSchedulingGroup(ctx context.Context, account *Account, groupID *int64) bool {
	if allowed, scoped := groupInputRoutingAccountAllowed(ctx, groupID, account); scoped {
		return allowed
	}
	return s.isAccountInGroup(account, groupID)
}

func (s *OpenAIGatewayService) openAIAccountMatchesInputRoutingGroup(ctx context.Context, account *Account, groupID *int64) bool {
	if allowed, scoped := groupInputRoutingAccountAllowed(ctx, groupID, account); scoped {
		return allowed
	}
	return s.openAIAccountMatchesSchedulingGroup(account, groupID)
}

type openAIInputRoutingSelection struct {
	selection *AccountSelectionResult
	decision  OpenAIAccountScheduleDecision
}

func (s *defaultOpenAIAccountScheduler) Select(ctx context.Context, req OpenAIAccountScheduleRequest) (*AccountSelectionResult, OpenAIAccountScheduleDecision, error) {
	result, err := selectWithGroupInputRouting(ctx, req.GroupID,
		func(ctx context.Context) (context.Context, error) {
			return s.service.prepareInputRouting(ctx, req.GroupID, req.Platform, req.RequestedModel, req.RequireCompact)
		},
		func(ctx context.Context) (openAIInputRoutingSelection, error) {
			selection, decision, err := s.selectOnce(ctx, req)
			return openAIInputRoutingSelection{selection, decision}, err
		})
	return result.selection, result.decision, err
}

func (s *OpenAIGatewayService) selectAccountWithScheduler(ctx context.Context, groupID *int64, previousResponseID, sessionHash, requestedModel string, excludedIDs map[int64]struct{}, requiredTransport OpenAIUpstreamTransport, requiredCapability OpenAIEndpointCapability, requiredImageCapability OpenAIImagesCapability, requireCompact bool, platform string, previousResponseCanMove, useUpstreamTokenCost bool) (*AccountSelectionResult, OpenAIAccountScheduleDecision, error) {
	result, err := selectWithGroupInputRouting(ctx, groupID,
		func(ctx context.Context) (context.Context, error) {
			return s.prepareInputRouting(ctx, groupID, platform, requestedModel, requireCompact)
		},
		func(ctx context.Context) (openAIInputRoutingSelection, error) {
			selection, decision, err := s.selectAccountWithSchedulerScoped(ctx, groupID, previousResponseID, sessionHash, requestedModel, excludedIDs, requiredTransport, requiredCapability, requiredImageCapability, requireCompact, platform, previousResponseCanMove, useUpstreamTokenCost)
			return openAIInputRoutingSelection{selection, decision}, err
		})
	return result.selection, result.decision, err
}

type openAIInputRoutingStickySelection struct {
	account *Account
	sticky  bool
}

func (s *OpenAIGatewayService) selectAccountForModelWithExclusionsStickyHit(ctx context.Context, groupID *int64, platform, sessionHash, requestedModel string, excludedIDs map[int64]struct{}, requireCompact bool, stickyAccountID int64, requiredCapability OpenAIEndpointCapability, preferLowUpstreamRate bool) (*Account, bool, error) {
	result, err := selectWithGroupInputRouting(ctx, groupID,
		func(ctx context.Context) (context.Context, error) {
			return s.prepareInputRouting(ctx, groupID, platform, requestedModel, requireCompact)
		},
		func(ctx context.Context) (openAIInputRoutingStickySelection, error) {
			account, sticky, err := s.selectAccountForModelWithExclusionsStickyHitOnce(ctx, groupID, platform, sessionHash, requestedModel, excludedIDs, requireCompact, stickyAccountID, requiredCapability, preferLowUpstreamRate)
			return openAIInputRoutingStickySelection{account, sticky}, err
		})
	return result.account, result.sticky, err
}

func (s *OpenAIGatewayService) selectAccountWithLoadAwareness(ctx context.Context, groupID *int64, platform, sessionHash, requestedModel string, excludedIDs map[int64]struct{}, requireCompact bool, requiredCapability OpenAIEndpointCapability, useUpstreamTokenCost bool) (*AccountSelectionResult, error) {
	return selectWithGroupInputRouting(ctx, groupID,
		func(ctx context.Context) (context.Context, error) {
			return s.prepareInputRouting(ctx, groupID, platform, requestedModel, requireCompact)
		},
		func(ctx context.Context) (*AccountSelectionResult, error) {
			return s.selectAccountWithLoadAwarenessOnce(ctx, groupID, platform, sessionHash, requestedModel, excludedIDs, requireCompact, requiredCapability, useUpstreamTokenCost)
		})
}

func inputRoutingHydrationAllowed(ctx context.Context, account *Account) bool {
	request := groupInputRoutingRequestFromContext(ctx)
	if request == nil {
		return true
	}
	allowed, _ := groupInputRoutingAccountAllowed(ctx, &request.decision.SourceGroupID, account)
	return allowed
}

// groupInputRoutingAdmissionLatest rechecks after a wait slot was acquired.
// Routing cannot fail open on a missing account/group: that would allow a
// removed target to execute merely because its old snapshot was selectable.
func groupInputRoutingAdmissionLatest(ctx context.Context, selected *Account, snapshot *SchedulerSnapshotService) (*Account, bool, string) {
	request := groupInputRoutingRequestFromContext(ctx)
	pass := groupInputRoutingPassFromContext(ctx)
	if selected == nil || request == nil || pass == nil || snapshot == nil {
		return selected, true, "input_routing_scope_unavailable"
	}
	var latest *Account
	var err error
	if snapshot.accountRepo != nil {
		latest, err = snapshot.accountRepo.GetByID(ctx, selected.ID)
	} else {
		latest, err = snapshot.GetAccount(ctx, selected.ID)
	}
	if err != nil || latest == nil {
		return selected, true, "input_routing_account_unavailable"
	}
	if !latest.IsSchedulable() || !latest.IsSchedulableForModelWithContext(ctx, pass.requestedModel) {
		return latest, true, "input_routing_account_unavailable"
	}
	if pass.validateAccount != nil && !pass.validateAccount(ctx, latest) {
		return latest, true, "input_routing_native_eligibility"
	}
	if pass.requestedModel != "" {
		if latest.IsOpenAICompatible() {
			if !supportsOpenAIRequestModel(latest, pass.requestedModel, pass.requireCompact) {
				return latest, true, "input_routing_model_unsupported"
			}
		} else if !(&GatewayService{}).isModelSupportedByAccountWithContext(ctx, latest, pass.requestedModel) {
			return latest, true, "input_routing_model_unsupported"
		}
	}
	updatedPass := *pass
	updatedPass.groups = make(map[int64]*Group, len(pass.groups))
	if !pass.original {
		if _, direct := pass.direct[latest.ID]; !direct {
			for id := range pass.groups {
				if !openAIStickyAccountMatchesGroup(latest, &id) {
					continue
				}
				group, groupErr := snapshot.GetGroupByIDLite(ctx, id)
				if groupErr == nil && groupInputRoutingTargetGroupAllowed(ctx, request, group, pass.platform, pass.model) {
					updatedPass.groups[id] = group
				}
			}
		}
	}
	admissionCtx := context.WithValue(ctx, groupInputRoutingPassKey{}, &updatedPass)
	if allowed, _ := groupInputRoutingAccountAllowed(admissionCtx, &request.decision.SourceGroupID, latest); !allowed {
		return latest, true, "input_routing_scope"
	}
	return latest, false, ""
}
