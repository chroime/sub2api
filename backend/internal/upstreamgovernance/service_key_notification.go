package upstreamgovernance

import (
	"context"
	"time"
)

type KeyNotice struct {
	Kind          string
	SiteID        int64
	SiteName      string
	BaseURL       string
	RemoteGroupID string
	RemoteKeyID   string
	ObservedAt    time.Time
}

type KeyNotifier interface {
	Recipients(context.Context, []string) ([]string, error)
	SendKey(context.Context, string, KeyNotice) error
}

func (s *Service) SetKeyNotifier(notifier KeyNotifier) { s.keyNotifier = notifier }

// Delivery is reserved before SMTP. Each recipient has an independent hash-
// keyed receipt so a failed recipient does not duplicate successful messages.
func (s *Service) notifyKeyHealth(ctx context.Context, store KeyHealthStore, site Site, key ManagedKey, health *KeyHealth) {
	if health.NotificationKind == "" || health.NotificationStatus == "sent" {
		return
	}
	now := s.now().UTC()
	retryAt := now.Add(15 * time.Minute)
	if s.keyNotifier == nil {
		health.NotificationStatus = "unavailable"
		health.NextCheckAt = &retryAt
		_ = store.SaveKeyHealth(ctx, key, *health)
		return
	}
	recipients, err := s.keyNotifier.Recipients(ctx, nil)
	if err == nil {
		recipients, err = normalizeBalanceRecipients(recipients)
	}
	if err != nil || len(recipients) == 0 {
		health.NotificationStatus = "unavailable"
		health.NextCheckAt = &retryAt
		_ = store.SaveKeyHealth(ctx, key, *health)
		return
	}
	if health.NotificationRecipients == nil {
		health.NotificationRecipients = map[string]BalanceRecipientState{}
	}
	anyFailed := false
	for _, recipient := range recipients {
		if ctx.Err() != nil {
			return
		}
		hash := balanceRecipientHash(recipient)
		state := health.NotificationRecipients[hash]
		if state.LastSentAt != nil {
			continue
		}
		if state.NextAttemptAt.After(now) {
			anyFailed = anyFailed || state.Failed
			continue
		}
		state.NextAttemptAt = now.Add(15 * time.Minute)
		health.NotificationRecipients[hash] = state
		health.NotificationStatus = "reserved"
		health.NotificationReservedAt = &now
		health.NextCheckAt = &retryAt
		if err = store.SaveKeyHealth(ctx, key, *health); err != nil {
			return
		}
		notice := KeyNotice{Kind: health.NotificationKind, SiteID: site.ID, SiteName: site.Name, BaseURL: site.BaseURL, RemoteGroupID: key.RemoteGroupID, RemoteKeyID: key.RemoteKeyID, ObservedAt: now}
		err = s.keyNotifier.SendKey(ctx, recipient, notice)
		state.Failed = err != nil
		if err == nil {
			state.LastSentAt = &now
		} else {
			anyFailed = true
		}
		health.NotificationRecipients[hash] = state
		persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		_ = store.SaveKeyHealth(persistCtx, key, *health)
		cancel()
	}
	allSent := true
	for _, recipient := range recipients {
		if health.NotificationRecipients[balanceRecipientHash(recipient)].LastSentAt == nil {
			allSent = false
		}
	}
	if allSent {
		health.NotificationStatus = "sent"
		health.NotificationSentAt = &now
		health.NextCheckAt = nil
	} else if anyFailed {
		health.NotificationStatus = "failed"
		health.NextCheckAt = &retryAt
	}
	_ = store.SaveKeyHealth(ctx, key, *health)
}
