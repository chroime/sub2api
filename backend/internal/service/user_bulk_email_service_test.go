package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/stretchr/testify/require"
)

type bulkEmailUsersStub struct {
	UserRepository
	users     map[int64]*User
	pages     map[int][]User
	pageCalls []pagination.PaginationParams
	filters   []UserListFilters
}

func (r *bulkEmailUsersStub) GetByID(_ context.Context, id int64) (*User, error) {
	if user, ok := r.users[id]; ok {
		return user, nil
	}
	return nil, ErrUserNotFound
}

func (r *bulkEmailUsersStub) ListWithFilters(_ context.Context, params pagination.PaginationParams, filters UserListFilters) ([]User, *pagination.PaginationResult, error) {
	r.pageCalls = append(r.pageCalls, params)
	r.filters = append(r.filters, filters)
	return r.pages[params.Page], nil, nil
}

type bulkEmailSenderStub struct {
	sent         []NotificationEmailSendInput
	previews     []NotificationEmailSendInput
	unsubscribed map[string]bool
	fail         map[string]bool
	suppressed   map[string]bool
	links        *NotificationEmailLinks
	linksErr     error
}

func (s *bulkEmailSenderStub) SendDelivered(_ context.Context, input NotificationEmailSendInput) (bool, error) {
	s.sent = append(s.sent, input)
	if s.fail[input.RecipientEmail] {
		return false, errors.New("SMTP delivery rejected")
	}
	return !s.suppressed[input.RecipientEmail], nil
}
func (s *bulkEmailSenderStub) PreviewSend(_ context.Context, input NotificationEmailSendInput) (NotificationEmailPreview, error) {
	s.previews = append(s.previews, input)
	return NotificationEmailPreview{Subject: "Health", HTML: input.RawHTMLVariables["group_summary_html"]}, nil
}
func (s *bulkEmailSenderStub) IsUnsubscribed(_ context.Context, email, _ string) (bool, error) {
	return s.unsubscribed[email], nil
}
func (s *bulkEmailSenderStub) ResolveRecipientLocale(context.Context, int64, string) string {
	return "zh"
}

func bulkEmailTestLinks() NotificationEmailLinks {
	return NotificationEmailLinks{PlatformURL: "https://site.example", DocsURL: "https://site.example/docs", ModelPlazaURL: "https://site.example/model-plaza", CanvasURL: "https://canvas.example"}
}

func (s *bulkEmailSenderStub) ResolveServiceLinks(context.Context) (NotificationEmailLinks, error) {
	if s.links != nil {
		return *s.links, s.linksErr
	}
	return bulkEmailTestLinks(), s.linksErr
}

type bulkEmailGroupsStub struct {
	groups []Group
	rates  map[int64]float64
	err    error
}

func (s *bulkEmailGroupsStub) GetAvailableGroups(context.Context, int64) ([]Group, error) {
	return s.groups, s.err
}
func (s *bulkEmailGroupsStub) GetUserGroupRates(context.Context, int64) (map[int64]float64, error) {
	return s.rates, nil
}

type bulkEmailAccountsStub struct {
	accounts map[int64][]Account
	errors   map[int64]error
	calls    map[int64]int
}

func (s *bulkEmailAccountsStub) ListByGroup(_ context.Context, id int64) ([]Account, error) {
	if s.calls == nil {
		s.calls = map[int64]int{}
	}
	s.calls[id]++
	return s.accounts[id], s.errors[id]
}

func TestUserBulkEmailSelectedDeduplicatesAndReportsEachDelivery(t *testing.T) {
	now := time.Now()
	users := &bulkEmailUsersStub{users: map[int64]*User{
		1: {ID: 1, Email: "sent@example.com"},
		2: {ID: 2, Email: "failed@example.com"},
		3: {ID: 3, Email: "optout@example.com"},
		4: {ID: 4, Email: "user" + OIDCConnectSyntheticEmailDomain},
		5: {ID: 5, Email: "deleted@example.com", DeletedAt: &now},
		6: {ID: 6, Email: "Name <another@example.com>"},
	}}
	sender := &bulkEmailSenderStub{unsubscribed: map[string]bool{"optout@example.com": true}, fail: map[string]bool{"failed@example.com": true}}
	svc := &UserBulkEmailService{users: users, sender: sender}
	result, err := svc.Send(t.Context(), UserBulkEmailInput{UserIDs: []int64{1, 2, 1, 3, 4, 5, 6, 7}, Event: NotificationEmailEventSystemHealthSnapshot})
	require.NoError(t, err)
	require.Equal(t, 7, result.Total)
	require.Equal(t, 1, result.Sent)
	require.Equal(t, 1, result.Failed)
	require.Equal(t, 5, result.Skipped)
	require.Len(t, sender.sent, 2)
	require.Equal(t, "zh", sender.sent[0].Locale)
	require.Equal(t, "failed", result.Items[1].Status)
	require.Equal(t, "email_delivery_failed", result.Items[1].Error)
	require.Equal(t, "unsubscribed", result.Items[2].Error)
	require.Equal(t, "invalid_email", result.Items[3].Error)
	require.Equal(t, "user_not_found", result.Items[4].Error)
	require.Empty(t, sender.sent[0].SourceID, "manual sends must not silently deduplicate earlier deliveries")
}

func TestUserBulkEmailSuppressedDeliveryIsNotCountedAsSent(t *testing.T) {
	users := &bulkEmailUsersStub{users: map[int64]*User{1: {ID: 1, Email: "suppressed@example.com"}}}
	sender := &bulkEmailSenderStub{suppressed: map[string]bool{"suppressed@example.com": true}}
	svc := &UserBulkEmailService{users: users, sender: sender}
	result, err := svc.Send(t.Context(), UserBulkEmailInput{UserIDs: []int64{1}, Event: NotificationEmailEventSystemHealthSnapshot})
	require.NoError(t, err)
	require.Zero(t, result.Sent)
	require.Equal(t, 1, result.Skipped)
	require.Equal(t, "notification_suppressed", result.Items[0].Error)
}

func TestUserBulkEmailRejectsUnsupportedEventsAndRecipientModes(t *testing.T) {
	sender := &bulkEmailSenderStub{}
	svc := &UserBulkEmailService{users: &bulkEmailUsersStub{}, sender: sender}
	for _, input := range []UserBulkEmailInput{
		{UserIDs: []int64{1}, Event: NotificationEmailEventAuthVerifyCode},
		{UserIDs: []int64{1}, Event: NotificationEmailEventBalanceRechargeSuccess},
		{Event: NotificationEmailEventSystemHealthSnapshot},
		{All: true, UserIDs: []int64{1}, Event: NotificationEmailEventSystemHealthSnapshot},
		{UserIDs: []int64{-1}, Event: NotificationEmailEventSystemHealthSnapshot},
		{UserIDs: []int64{1}, Event: NotificationEmailEventSystemHealthSnapshot, Locale: "unsupported"},
	} {
		_, err := svc.Send(t.Context(), input)
		require.ErrorIs(t, err, ErrUserBulkEmailInvalid)
	}
	require.Empty(t, sender.sent)
}

func TestUserBulkEmailAllPaginatesWithoutDeletedUsers(t *testing.T) {
	users := &bulkEmailUsersStub{users: map[int64]*User{}, pages: map[int][]User{}}
	for id := int64(1); id <= 102; id++ {
		user := User{ID: id, Email: fmt.Sprintf("user%d@example.com", id)}
		users.users[id] = &user
		page := int((id-1)/100) + 1
		users.pages[page] = append(users.pages[page], user)
	}
	users.pages[2] = append(users.pages[2], users.pages[1][0])
	sender := &bulkEmailSenderStub{}
	svc := &UserBulkEmailService{users: users, sender: sender}
	result, err := svc.Send(t.Context(), UserBulkEmailInput{All: true, Event: NotificationEmailEventSystemHealthSnapshot})
	require.NoError(t, err)
	require.Equal(t, 102, result.Total)
	require.Equal(t, 102, result.Sent)
	require.Len(t, users.pageCalls, 2)
	require.Equal(t, 2, users.pageCalls[1].Page)
	require.Equal(t, "id", users.pageCalls[0].SortBy)
	require.Equal(t, "asc", users.pageCalls[0].SortOrder)
	for _, filter := range users.filters {
		require.False(t, filter.IncludeDeleted)
		require.NotNil(t, filter.IncludeSubscriptions)
		require.False(t, *filter.IncludeSubscriptions)
	}
}

func TestUserBulkEmailHealthUsesRealAvailabilityAndEscapesGroupContent(t *testing.T) {
	groups := &bulkEmailGroupsStub{groups: []Group{
		{ID: 1, Name: "<img src=x onerror=alert(1)>", Description: "<script>bad</script>", Status: StatusActive, RateMultiplier: 2},
		{ID: 2, Name: "Busy", Status: StatusActive, RateMultiplier: 1},
		{ID: 3, Name: "Unknown", Status: StatusActive, Platform: PlatformComposite},
		{ID: 4, Name: "Disabled", Status: StatusDisabled},
	}, rates: map[int64]float64{1: 0.25}}
	future := time.Now().Add(time.Hour)
	accounts := &bulkEmailAccountsStub{accounts: map[int64][]Account{1: {{Status: StatusActive, Schedulable: true}}, 2: {{Status: StatusActive, Schedulable: true, RateLimitResetAt: &future}}}}
	svc := &UserBulkEmailService{groups: groups, accounts: accounts}
	vars, table := svc.healthVariables(t.Context(), 1, "zh", map[int64]userBulkGroupHealth{})
	require.Equal(t, "需关注", vars["health_status"])
	require.Regexp(t, `^20[0-9]{2}-[0-9]{2}-[0-9]{2} [0-9]{2}:[0-9]{2}:[0-9]{2} \(UTC\+8\)$`, vars["health_checked_at"])
	checkedAt, err := time.ParseInLocation("2006-01-02 15:04:05", strings.TrimSuffix(vars["health_checked_at"], " (UTC+8)"), time.FixedZone("Beijing", 8*60*60))
	require.NoError(t, err)
	require.WithinDuration(t, time.Now(), checkedAt, 5*time.Second)
	require.Equal(t, "3", vars["active_group_count"])
	require.Contains(t, vars["health_status_detail"], "1 个状态未知")
	require.Contains(t, table, "0.25x")
	require.Contains(t, table, "&lt;script&gt;")
	require.NotContains(t, table, "<script>")
	require.NotContains(t, table, "<img")
	require.NotContains(t, table, "Disabled")
	require.NotContains(t, accounts.calls, int64(3), "composite group availability must not be invented from direct accounts")
}

type bulkEmailGroupRepoStub struct {
	GroupRepository
	groups []Group
}

func (r *bulkEmailGroupRepoStub) ListActive(context.Context) ([]Group, error) { return r.groups, nil }

type bulkEmailSubscriptionRepoStub struct {
	UserSubscriptionRepository
	subscriptions []UserSubscription
}

func (r *bulkEmailSubscriptionRepoStub) ListActiveByUserID(context.Context, int64) ([]UserSubscription, error) {
	return r.subscriptions, nil
}

func TestUserBulkEmailUsesExistingGroupPermissions(t *testing.T) {
	users := &bulkEmailUsersStub{users: map[int64]*User{1: {ID: 1, RestrictPublicGroups: true, AllowedGroups: []int64{2}}}}
	groups := &bulkEmailGroupRepoStub{groups: []Group{
		{ID: 1, Name: "Forbidden public", Status: StatusActive},
		{ID: 2, Name: "Allowed exclusive", Status: StatusActive, IsExclusive: true},
		{ID: 3, Name: "Forbidden exclusive", Status: StatusActive, IsExclusive: true},
		{ID: 4, Name: "Subscribed", Status: StatusActive, SubscriptionType: SubscriptionTypeSubscription},
		{ID: 5, Name: "Not subscribed", Status: StatusActive, SubscriptionType: SubscriptionTypeSubscription},
	}}
	subscriptions := &bulkEmailSubscriptionRepoStub{subscriptions: []UserSubscription{{GroupID: 4}}}
	keys := &APIKeyService{userRepo: users, groupRepo: groups, userSubRepo: subscriptions}
	svc := &UserBulkEmailService{groups: keys, accounts: &bulkEmailAccountsStub{}}
	variables, table := svc.healthVariables(t.Context(), 1, "en", map[int64]userBulkGroupHealth{})
	require.Equal(t, "2", variables["active_group_count"])
	require.Contains(t, table, "Allowed exclusive")
	require.Contains(t, table, "Subscribed")
	require.NotContains(t, table, "Forbidden")
	require.NotContains(t, table, "Not subscribed")
}

func TestUserBulkEmailUnknownDataNeverClaimsHealth(t *testing.T) {
	svc := &UserBulkEmailService{groups: &bulkEmailGroupsStub{err: errors.New("database unavailable")}}
	vars, _ := svc.healthVariables(t.Context(), 1, "en", map[int64]userBulkGroupHealth{})
	require.Equal(t, "Unknown", vars["health_status"])
	svc.groups = &bulkEmailGroupsStub{groups: []Group{{ID: 1, Status: StatusActive, Name: "Group"}}}
	svc.accounts = &bulkEmailAccountsStub{errors: map[int64]error{1: errors.New("database unavailable")}}
	vars, _ = svc.healthVariables(t.Context(), 1, "en", map[int64]userBulkGroupHealth{})
	require.Equal(t, "Unknown", vars["health_status"])
}

func TestUserBulkEmailPreviewUsesLiveVariablesAndDoesNotSend(t *testing.T) {
	sender := &bulkEmailSenderStub{}
	users := &bulkEmailUsersStub{users: map[int64]*User{10: {ID: 10, Email: "preview@example.com", Username: "Preview"}}}
	svc := &UserBulkEmailService{users: users, sender: sender, groups: &bulkEmailGroupsStub{groups: []Group{{ID: 1, Name: "Live group", Description: "Live description", Status: StatusActive, RateMultiplier: 0.75}}}, accounts: &bulkEmailAccountsStub{accounts: map[int64][]Account{1: {{Status: StatusActive, Schedulable: true}}}}}
	preview, err := svc.Preview(t.Context(), UserBulkEmailPreviewInput{UserID: 10, Event: NotificationEmailEventSystemHealthSnapshot})
	require.NoError(t, err)
	require.Contains(t, preview.HTML, "Live group")
	require.Contains(t, preview.HTML, "0.75x")
	require.Empty(t, sender.sent)
	require.Len(t, sender.previews, 1)
	require.Equal(t, "Preview", sender.previews[0].RecipientName)
	require.Equal(t, "可用", sender.previews[0].Variables["health_status"])
}

func TestUserBulkEmailServiceLinksMergeDefaultsAndExplicitOverrides(t *testing.T) {
	sender := &bulkEmailSenderStub{}
	users := &bulkEmailUsersStub{users: map[int64]*User{1: {ID: 1, Email: "user@example.com"}}}
	svc := &UserBulkEmailService{users: users, sender: sender}
	overrides := &NotificationEmailLinks{CanvasURL: " https://custom-canvas.example/project ", DocsURL: ""}
	preview, err := svc.Preview(t.Context(), UserBulkEmailPreviewInput{UserID: 1, Event: NotificationEmailEventSystemHealthSnapshot, Links: overrides})
	require.NoError(t, err)
	require.Equal(t, "https://custom-canvas.example/project", preview.Links.CanvasURL)
	require.Equal(t, bulkEmailTestLinks().DocsURL, preview.Links.DocsURL)
	require.Equal(t, bulkEmailTestLinks().PlatformURL, preview.Links.PlatformURL)
	require.Equal(t, bulkEmailTestLinks().ModelPlazaURL, preview.Links.ModelPlazaURL)
	require.Empty(t, sender.sent)
	result, err := svc.Send(t.Context(), UserBulkEmailInput{UserIDs: []int64{1}, Event: NotificationEmailEventSystemHealthSnapshot, Links: overrides})
	require.NoError(t, err)
	require.Equal(t, 1, result.Sent)
	require.Equal(t, preview.Links.Variables(), map[string]string{
		"platform_url":    sender.sent[0].Variables["platform_url"],
		"docs_url":        sender.sent[0].Variables["docs_url"],
		"model_plaza_url": sender.sent[0].Variables["model_plaza_url"],
		"canvas_url":      sender.sent[0].Variables["canvas_url"],
	})
}

func TestUserBulkEmailMissingServiceLinkSkipsSendButAllowsPreview(t *testing.T) {
	links := bulkEmailTestLinks()
	links.CanvasURL = ""
	sender := &bulkEmailSenderStub{links: &links}
	users := &bulkEmailUsersStub{users: map[int64]*User{1: {ID: 1, Email: "user@example.com"}}}
	svc := &UserBulkEmailService{users: users, sender: sender}
	result, err := svc.Send(t.Context(), UserBulkEmailInput{UserIDs: []int64{1}, Event: NotificationEmailEventSystemHealthSnapshot})
	require.NoError(t, err)
	require.Equal(t, 1, result.Skipped)
	require.Zero(t, result.Sent)
	require.Equal(t, "service_links_not_configured", result.Items[0].Error)
	require.Empty(t, sender.sent)
	preview, err := svc.Preview(t.Context(), UserBulkEmailPreviewInput{UserID: 1, Event: NotificationEmailEventSystemHealthSnapshot})
	require.NoError(t, err)
	require.Equal(t, links, preview.Links)
	require.Len(t, sender.previews, 1)
}

func TestUserBulkEmailRejectsUnsafeExplicitLinksBeforeSending(t *testing.T) {
	sender := &bulkEmailSenderStub{}
	users := &bulkEmailUsersStub{users: map[int64]*User{1: {ID: 1, Email: "user@example.com"}}}
	svc := &UserBulkEmailService{users: users, sender: sender}
	for _, value := range []string{"/relative", "//host.example/path", "javascript:alert(1)", "ftp://host.example", "https://user:password@host.example", "https://host.example\r\nInjected: yes", "\nhttps://host.example", "https:///path"} {
		t.Run(value, func(t *testing.T) {
			links := &NotificationEmailLinks{CanvasURL: value}
			_, err := svc.Send(t.Context(), UserBulkEmailInput{UserIDs: []int64{1}, Event: NotificationEmailEventSystemHealthSnapshot, Links: links})
			require.ErrorIs(t, err, ErrUserBulkEmailInvalid)
			_, err = svc.Preview(t.Context(), UserBulkEmailPreviewInput{UserID: 1, Event: NotificationEmailEventSystemHealthSnapshot, Links: links})
			require.ErrorIs(t, err, ErrUserBulkEmailInvalid)
		})
	}
	require.Empty(t, sender.sent)
	require.Empty(t, sender.previews)
	_, err := svc.Preview(t.Context(), UserBulkEmailPreviewInput{UserID: 1, Event: NotificationEmailEventSystemHealthSnapshot, Links: &NotificationEmailLinks{CanvasURL: "http://127.0.0.1:5174/canvas"}})
	require.NoError(t, err, "explicit localhost links are allowed for local tests")
}

func TestUserBulkEmailBalanceUsesCurrentBalanceAndOnlyNotifiesLowUsers(t *testing.T) {
	threshold := 20.0
	users := &bulkEmailUsersStub{users: map[int64]*User{
		1: {ID: 1, Email: "low@example.com", Balance: 3.25, BalanceNotifyEnabled: true, BalanceNotifyThreshold: &threshold},
		2: {ID: 2, Email: "high@example.com", Balance: 40, BalanceNotifyEnabled: true},
		3: {ID: 3, Email: "optout@example.com", Balance: 1, BalanceNotifyEnabled: false},
	}}
	repo := newNotificationEmailMemorySettingRepo()
	require.NoError(t, repo.SetMultiple(t.Context(), map[string]string{SettingKeyBalanceLowNotifyThreshold: "10", SettingKeyBalanceLowNotifyRechargeURL: "https://site.example/recharge"}))
	sender := &bulkEmailSenderStub{}
	svc := &UserBulkEmailService{users: users, sender: sender, settings: repo}
	result, err := svc.Send(t.Context(), UserBulkEmailInput{UserIDs: []int64{1, 2, 3}, Event: NotificationEmailEventBalanceLow})
	require.NoError(t, err)
	require.Equal(t, 1, result.Sent)
	require.Equal(t, 2, result.Skipped)
	require.Equal(t, "3.25", sender.sent[0].Variables["current_balance"])
	require.Equal(t, "20.00", sender.sent[0].Variables["threshold"])
	require.Equal(t, "balance_above_threshold", result.Items[1].Error)
}

func TestUserBulkEmailSMTPFailureAndUnsubscribeReflectRealDelivery(t *testing.T) {
	repo := newNotificationEmailMemorySettingRepo()
	smtpServer := startNotificationEmailTestSMTPServer(t)
	require.NoError(t, repo.SetMultiple(t.Context(), smtpServer.settings()))
	require.NoError(t, repo.Set(t.Context(), notificationEmailPreferenceKey(NotificationEmailEventSystemHealthSnapshot, "optout@example.com"), "unsubscribed"))
	sender := NewNotificationEmailService(repo, NewEmailService(repo, nil))
	users := &bulkEmailUsersStub{users: map[int64]*User{1: {ID: 1, Email: "sent@example.com"}, 2: {ID: 2, Email: "optout@example.com"}}}
	svc := &UserBulkEmailService{users: users, sender: sender}
	links := bulkEmailTestLinks()
	result, err := svc.Send(t.Context(), UserBulkEmailInput{UserIDs: []int64{1, 2}, Event: NotificationEmailEventSystemHealthSnapshot, Locale: "en", Links: &links})
	require.NoError(t, err)
	require.Equal(t, 1, result.Sent)
	require.Equal(t, 1, result.Skipped)
	require.Equal(t, int64(1), smtpServer.messageCount())
	require.True(t, strings.Contains(smtpServer.lastMessageBody(t), "Unknown"))
	// Missing SMTP configuration must never be counted as a successful send.
	badRepo := newNotificationEmailMemorySettingRepo()
	svc.sender = NewNotificationEmailService(badRepo, NewEmailService(badRepo, nil))
	result, err = svc.Send(t.Context(), UserBulkEmailInput{UserIDs: []int64{1}, Event: NotificationEmailEventSystemHealthSnapshot, Links: &links})
	require.NoError(t, err)
	require.Equal(t, 1, result.Failed)
	require.Zero(t, result.Sent)
}
