package service

import (
	"context"

	gov "github.com/Wei-Shaw/sub2api/internal/upstreamgovernance"
)

// governanceLocalGroupNotifier delivers site-independent local-group events
// only to configured AstrBot targets, without consulting email settings.
type governanceLocalGroupNotifier struct {
	base *governanceBalanceNotifier
}

var _ gov.LocalGroupChangeNotifier = (*governanceLocalGroupNotifier)(nil)

func (n *governanceLocalGroupNotifier) Recipients(ctx context.Context) ([]string, error) {
	if n == nil || n.base == nil {
		return nil, nil
	}
	return n.base.AstrBotRecipients(ctx)
}

func (n *governanceLocalGroupNotifier) SendLocalGroupChange(ctx context.Context, recipient string, notice gov.LocalGroupChangeNotice) error {
	if n == nil || n.base == nil {
		return ErrEmailNotConfigured
	}
	return n.base.SendChange(ctx, recipient, gov.ChangeNotice{
		Kind:        notice.Kind,
		Severity:    notice.Severity,
		DedupKey:    notice.DedupKey,
		Subject:     notice.Subject,
		Body:        notice.Body,
		AstrBotText: notice.AstrBotText,
		ObservedAt:  notice.ObservedAt,
	})
}
