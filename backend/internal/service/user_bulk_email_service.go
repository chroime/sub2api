package service

import (
	"context"
	"errors"
	"fmt"
	"html"
	"math"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/balancenotice"
	"github.com/google/uuid"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
)

const userBulkEmailPageSize = 100

var ErrUserBulkEmailInvalid = infraerrors.BadRequest("INVALID_BULK_EMAIL_REQUEST", "choose users or all users, and a supported notification event")

type UserBulkEmailInput struct {
	UserIDs []int64                 `json:"user_ids"`
	All     bool                    `json:"all"`
	Event   string                  `json:"event"`
	Locale  string                  `json:"locale"`
	Links   *NotificationEmailLinks `json:"links,omitempty"`
}

type UserBulkEmailPreviewInput struct {
	UserID int64                   `json:"user_id"`
	Event  string                  `json:"event"`
	Locale string                  `json:"locale"`
	Links  *NotificationEmailLinks `json:"links,omitempty"`
}

type UserBulkEmailPreviewResult struct {
	NotificationEmailPreview
	Links NotificationEmailLinks `json:"links"`
}

type UserBulkEmailItem struct {
	UserID int64  `json:"user_id"`
	Email  string `json:"email"`
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

type UserBulkEmailResult struct {
	Items   []UserBulkEmailItem `json:"items"`
	Total   int                 `json:"total"`
	Sent    int                 `json:"sent"`
	Failed  int                 `json:"failed"`
	Skipped int                 `json:"skipped"`
}

type userBulkEmailUsers interface {
	GetByID(context.Context, int64) (*User, error)
	ListWithFilters(context.Context, pagination.PaginationParams, UserListFilters) ([]User, *pagination.PaginationResult, error)
}

type userBulkEmailGroups interface {
	GetAvailableGroups(context.Context, int64) ([]Group, error)
	GetUserGroupRates(context.Context, int64) (map[int64]float64, error)
}

type userBulkEmailAccounts interface {
	ListByGroup(context.Context, int64) ([]Account, error)
}

type userBulkEmailSender interface {
	SendDelivered(context.Context, NotificationEmailSendInput) (bool, error)
	PreviewSend(context.Context, NotificationEmailSendInput) (NotificationEmailPreview, error)
	IsUnsubscribed(context.Context, string, string) (bool, error)
	ResolveRecipientLocale(context.Context, int64, string) string
	ResolveServiceLinks(context.Context) (NotificationEmailLinks, error)
}

// UserBulkEmailService sends only operator-triggered informational notices to
// registered user addresses. It never accepts arbitrary recipients or template
// variables from a request, and never issues verification/order emails.
type UserBulkEmailService struct {
	users    userBulkEmailUsers
	groups   userBulkEmailGroups
	accounts userBulkEmailAccounts
	settings SettingRepository
	sender   userBulkEmailSender
	records  *balancenotice.Service
}

func NewUserBulkEmailService(users UserRepository, groups *APIKeyService, accounts AccountRepository, settings SettingRepository, sender *NotificationEmailService) *UserBulkEmailService {
	return &UserBulkEmailService{users: users, groups: groups, accounts: accounts, settings: settings, sender: sender, records: sender.BalanceNotificationRecords()}
}

func normalizeUserBulkEmailEvent(event, locale string) (string, string, error) {
	event, locale = strings.TrimSpace(event), strings.TrimSpace(locale)
	if event != NotificationEmailEventSystemHealthSnapshot && event != NotificationEmailEventBalanceLow {
		return "", "", ErrUserBulkEmailInvalid
	}
	if locale != "" && locale != "zh" && locale != "en" {
		return "", "", ErrUserBulkEmailInvalid
	}
	return event, locale, nil
}

func (s *UserBulkEmailService) recipientIDs(ctx context.Context, input UserBulkEmailInput) ([]int64, error) {
	if input.All && len(input.UserIDs) > 0 || !input.All && (len(input.UserIDs) == 0 || len(input.UserIDs) > 500) {
		return nil, ErrUserBulkEmailInvalid
	}
	ids := make([]int64, 0, len(input.UserIDs))
	seen := make(map[int64]bool)
	appendID := func(id int64) error {
		if id <= 0 {
			return ErrUserBulkEmailInvalid
		}
		if !seen[id] {
			ids, seen[id] = append(ids, id), true
		}
		return nil
	}
	if !input.All {
		for _, id := range input.UserIDs {
			if err := appendID(id); err != nil {
				return nil, err
			}
		}
		return ids, nil
	}
	includeSubscriptions := false
	for page := 1; ; page++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		users, pager, err := s.users.ListWithFilters(ctx, pagination.PaginationParams{Page: page, PageSize: userBulkEmailPageSize, SortBy: "id", SortOrder: pagination.SortOrderAsc}, UserListFilters{IncludeSubscriptions: &includeSubscriptions})
		if err != nil {
			return nil, fmt.Errorf("list bulk email recipients: %w", err)
		}
		for _, user := range users {
			if user.DeletedAt == nil {
				if err := appendID(user.ID); err != nil {
					return nil, err
				}
			}
		}
		if len(users) == 0 || len(users) < userBulkEmailPageSize || pager != nil && int64(page*userBulkEmailPageSize) >= pager.Total {
			break
		}
	}
	return ids, nil
}

func (s *UserBulkEmailService) Send(ctx context.Context, input UserBulkEmailInput) (*UserBulkEmailResult, error) {
	event, locale, err := normalizeUserBulkEmailEvent(input.Event, input.Locale)
	if err != nil {
		return nil, err
	}
	if err := validateUserBulkEmailLinks(input.Links); err != nil {
		return nil, err
	}
	if s == nil || s.users == nil || s.sender == nil {
		return nil, errors.New("bulk email service is not configured")
	}
	ids, err := s.recipientIDs(ctx, input)
	if err != nil {
		return nil, err
	}
	result := &UserBulkEmailResult{Items: make([]UserBulkEmailItem, 0, len(ids)), Total: len(ids)}
	health := make(map[int64]userBulkGroupHealth)
	batchID := uuid.NewString()
	for _, id := range ids {
		item := s.sendUser(ctx, id, event, locale, health, input.Links, batchID)
		result.Items = append(result.Items, item)
		switch item.Status {
		case "sent":
			result.Sent++
		case "skipped":
			result.Skipped++
		default:
			result.Failed++
		}
	}
	return result, nil
}

func (s *UserBulkEmailService) sendUser(ctx context.Context, id int64, event, locale string, health map[int64]userBulkGroupHealth, links *NotificationEmailLinks, batchIDs ...string) (item UserBulkEmailItem) {
	item = UserBulkEmailItem{UserID: id, Status: "failed"}
	var delivery *balancenotice.Delivery
	var knownUser *User
	amountsResolved := false
	batchID := ""
	if len(batchIDs) > 0 {
		batchID = batchIDs[0]
	}
	defer func() {
		if event != NotificationEmailEventBalanceLow {
			return
		}
		if delivery == nil {
			delivery = s.records.Begin(balancenotice.Input{UserID: id, TriggerType: "manual_batch", BatchID: batchID})
		}
		if s.records != nil && knownUser != nil && !amountsResolved {
			delivery.Amounts(&knownUser.Balance, s.balanceThresholdForRecord(knownUser))
		}
		if delivery != nil {
			if item.Status == "skipped" {
				delivery.Suppress(item.Error)
			} else if item.Status == "failed" {
				delivery.Fail(errors.New(item.Error))
			}
		}
	}()
	if err := ctx.Err(); err != nil {
		item.Error = "request_cancelled"
		return item
	}
	user, err := s.users.GetByID(ctx, id)
	if errors.Is(err, ErrUserNotFound) || err == nil && (user == nil || user.DeletedAt != nil) {
		item.Status, item.Error = "skipped", "user_not_found"
		return item
	}
	if err != nil {
		item.Error = "user_lookup_failed"
		return item
	}
	item.Email = strings.TrimSpace(user.Email)
	knownUser = user
	if event == NotificationEmailEventBalanceLow {
		delivery = s.records.Begin(balancenotice.Input{UserID: user.ID, UserEmail: user.Email, UserName: user.Username, Recipient: item.Email, Balance: &user.Balance, TriggerType: "manual_batch", BatchID: batchID})
	}
	if !userBulkEmailAddressValid(item.Email) {
		item.Status, item.Error = "skipped", "invalid_email"
		return item
	}
	unsubscribed, err := s.sender.IsUnsubscribed(ctx, item.Email, event)
	if err != nil {
		item.Error = "notification_preference_unavailable"
		return item
	}
	if unsubscribed || event == NotificationEmailEventBalanceLow && !user.BalanceNotifyEnabled {
		item.Status, item.Error = "skipped", "unsubscribed"
		return item
	}
	mailInput, reason, err := s.messageInput(ctx, user, event, locale, health, links, delivery)
	amountsResolved = err == nil
	mailInput.BalanceDelivery = delivery
	if err != nil {
		item.Error = "notification_data_unavailable"
		return item
	}
	if reason != "" {
		item.Status, item.Error = "skipped", reason
		return item
	}
	// No SourceID is supplied: each explicit administrator send is a new
	// delivery, so a historical deduplication key cannot masquerade as SMTP success.
	delivered, err := s.sender.SendDelivered(ctx, mailInput)
	if err != nil {
		item.Error = "email_delivery_failed"
		return item
	}
	if !delivered {
		item.Status, item.Error = "skipped", "notification_suppressed"
		return item
	}
	item.Status = "sent"
	return item
}

func userBulkEmailAddressValid(email string) bool {
	address, err := mail.ParseAddress(email)
	if err != nil || address.Address != email || strings.ContainsAny(email, "\r\n") || isReservedEmail(email) {
		return false
	}
	parts := strings.Split(email, "@")
	if len(parts) != 2 || !strings.Contains(parts[1], ".") {
		return false
	}
	domain := strings.ToLower(strings.TrimSuffix(parts[1], "."))
	return domain != "invalid" && !strings.HasSuffix(domain, ".invalid") && domain != "localhost" && !strings.HasSuffix(domain, ".localhost")
}

func (s *UserBulkEmailService) Preview(ctx context.Context, input UserBulkEmailPreviewInput) (UserBulkEmailPreviewResult, error) {
	event, locale, err := normalizeUserBulkEmailEvent(input.Event, input.Locale)
	if err != nil || input.UserID <= 0 {
		return UserBulkEmailPreviewResult{}, ErrUserBulkEmailInvalid
	}
	if err := validateUserBulkEmailLinks(input.Links); err != nil {
		return UserBulkEmailPreviewResult{}, err
	}
	if s == nil || s.users == nil || s.sender == nil {
		return UserBulkEmailPreviewResult{}, errors.New("bulk email service is not configured")
	}
	user, err := s.users.GetByID(ctx, input.UserID)
	if err != nil {
		return UserBulkEmailPreviewResult{}, err
	}
	if user == nil || user.DeletedAt != nil {
		return UserBulkEmailPreviewResult{}, ErrUserNotFound
	}
	message, _, err := s.messageInput(ctx, user, event, locale, map[int64]userBulkGroupHealth{}, input.Links)
	if err != nil {
		return UserBulkEmailPreviewResult{}, err
	}
	preview, err := s.sender.PreviewSend(ctx, message)
	if err != nil {
		return UserBulkEmailPreviewResult{}, err
	}
	return UserBulkEmailPreviewResult{NotificationEmailPreview: preview, Links: NotificationEmailLinks{
		PlatformURL: message.Variables["platform_url"], DocsURL: message.Variables["docs_url"],
		ModelPlazaURL: message.Variables["model_plaza_url"], CanvasURL: message.Variables["canvas_url"],
	}}, nil
}

func (s *UserBulkEmailService) messageInput(ctx context.Context, user *User, event, locale string, health map[int64]userBulkGroupHealth, overrides *NotificationEmailLinks, deliveries ...*balancenotice.Delivery) (NotificationEmailSendInput, string, error) {
	if locale == "" {
		locale = s.sender.ResolveRecipientLocale(ctx, user.ID, user.Email)
	}
	input := NotificationEmailSendInput{Event: event, Locale: locale, UserID: user.ID, RecipientEmail: strings.TrimSpace(user.Email), RecipientName: user.Username, Variables: map[string]string{}, RawHTMLVariables: map[string]string{}}
	if input.RecipientName == "" {
		input.RecipientName = input.RecipientEmail
	}
	if event == NotificationEmailEventBalanceLow {
		if s.settings == nil {
			return input, "", errors.New("balance notification settings unavailable")
		}
		settings, err := s.settings.GetMultiple(ctx, []string{SettingKeyBalanceLowNotifyThreshold, SettingKeyBalanceLowNotifyRechargeURL})
		if err != nil {
			return input, "", err
		}
		threshold, _ := strconv.ParseFloat(settings[SettingKeyBalanceLowNotifyThreshold], 64)
		if user.BalanceNotifyThreshold != nil {
			threshold = *user.BalanceNotifyThreshold
		}
		threshold = resolveBalanceThreshold(threshold, user.BalanceNotifyThresholdType, user.TotalRecharged)
		if len(deliveries) > 0 {
			deliveries[0].Amounts(&user.Balance, &threshold)
		}
		input.Variables["current_balance"] = fmt.Sprintf("%.2f", user.Balance)
		input.Variables["threshold"] = fmt.Sprintf("%.2f", threshold)
		input.Variables["recharge_url"] = settings[SettingKeyBalanceLowNotifyRechargeURL]
		if math.IsNaN(threshold) || math.IsInf(threshold, 0) || threshold <= 0 {
			return input, "balance_threshold_not_configured", nil
		}
		if user.Balance >= threshold {
			return input, "balance_above_threshold", nil
		}
		return input, "", nil
	}
	variables, table := s.healthVariables(ctx, user.ID, locale, health)
	input.Variables = variables
	input.RawHTMLVariables["group_summary_html"] = table
	links, err := s.sender.ResolveServiceLinks(ctx)
	if err != nil {
		return input, "", err
	}
	if overrides != nil {
		if value := strings.TrimSpace(overrides.PlatformURL); value != "" {
			links.PlatformURL = value
		}
		if value := strings.TrimSpace(overrides.DocsURL); value != "" {
			links.DocsURL = value
		}
		if value := strings.TrimSpace(overrides.ModelPlazaURL); value != "" {
			links.ModelPlazaURL = value
		}
		if value := strings.TrimSpace(overrides.CanvasURL); value != "" {
			links.CanvasURL = value
		}
	}
	for key, value := range links.Variables() {
		value = strings.TrimSpace(value)
		input.Variables[key] = value
	}
	if strings.TrimSpace(links.PlatformURL) == "" || strings.TrimSpace(links.DocsURL) == "" || strings.TrimSpace(links.ModelPlazaURL) == "" || strings.TrimSpace(links.CanvasURL) == "" {
		return input, "service_links_not_configured", nil
	}
	return input, "", nil
}

func validateUserBulkEmailLinks(links *NotificationEmailLinks) error {
	if links == nil {
		return nil
	}
	for _, raw := range links.Variables() {
		if strings.ContainsAny(raw, "\r\n") {
			return ErrUserBulkEmailInvalid
		}
		value := strings.TrimSpace(raw)
		if value == "" {
			continue
		}
		parsed, err := url.ParseRequestURI(value)
		if err != nil || !parsed.IsAbs() || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" || parsed.User != nil {
			return ErrUserBulkEmailInvalid
		}
	}
	return nil
}

type userBulkGroupHealth struct {
	Known     bool
	Available int
	Total     int
}

func (s *UserBulkEmailService) healthVariables(ctx context.Context, userID int64, locale string, cache map[int64]userBulkGroupHealth) (map[string]string, string) {
	zh := normalizeNotificationLocale(locale) == notificationEmailLocaleChinese
	label := func(en, cn string) string {
		if zh {
			return cn
		}
		return en
	}
	now := time.Now()
	variables := map[string]string{
		"health_status":        label("Unknown", "未知"),
		"health_status_detail": label("No verified group availability data is available.", "暂无可核实的分组可用性数据。"),
		"health_checked_at":    formatEmailTime(now) + " (UTC+8)",
		"active_group_count":   "0",
		"group_summary_html":   "",
	}
	empty := "<p>" + html.EscapeString(label("No available group information.", "暂无可用分组信息。")) + "</p>"
	if s.groups == nil {
		return variables, empty
	}
	groups, err := s.groups.GetAvailableGroups(ctx, userID)
	if err != nil {
		return variables, empty
	}
	rates, rateErr := s.groups.GetUserGroupRates(ctx, userID)
	var rows strings.Builder
	known, available, active := 0, 0, 0
	headers := groupUpdateEmailHeaders(locale)
	for _, group := range groups {
		if !group.IsActive() {
			continue
		}
		active++
		state, ok := cache[group.ID]
		if !ok {
			if group.Platform != PlatformComposite && s.accounts != nil {
				accounts, accountErr := s.accounts.ListByGroup(ctx, group.ID)
				if accountErr == nil {
					state.Known, state.Total = true, len(accounts)
					for i := range accounts {
						if accounts[i].IsSchedulable() && !accounts[i].IsSyntheticUITest() {
							state.Available++
						}
					}
				}
			}
			cache[group.ID] = state
		}
		if state.Known {
			known++
			if state.Available > 0 {
				available++
			}
		}
		rateText := label("Unknown", "未知")
		if rateErr == nil {
			rate := group.RateMultiplier
			if override, ok := rates[group.ID]; ok {
				rate = override
			}
			rate *= group.PeakMultiplierAt(now)
			if rate >= 0 && !math.IsNaN(rate) && !math.IsInf(rate, 0) {
				rateText = strconv.FormatFloat(rate, 'f', -1, 64) + "x"
			}
		}
		rows.WriteString(groupUpdateEmailRow(headers, []string{group.Name, group.Description, rateText}))
	}
	variables["active_group_count"] = strconv.Itoa(active)
	if active == 0 {
		return variables, empty
	}
	switch {
	case known == active && available == active:
		variables["health_status"] = label("Available", "可用")
	case known > available:
		variables["health_status"] = label("Attention needed", "需关注")
	}
	variables["health_status_detail"] = label(
		fmt.Sprintf("%d of %d authorized enabled groups have schedulable accounts; %d have unknown availability. This is an account-state snapshot, not an upstream request test.", available, active, active-known),
		fmt.Sprintf("您有权限的 %d 个启用分组中，%d 个有可调度账号，%d 个状态未知。此结果为账号运行状态快照，并非上游请求实测。", active, available, active-known),
	)
	return variables, groupUpdateEmailTable(headers, rows.String())
}
