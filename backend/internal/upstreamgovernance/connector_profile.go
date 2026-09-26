package upstreamgovernance

import (
	"context"
	"errors"
	"math"
)

func (c *platformConnector) profile(ctx context.Context, s Site, session Session) (Session, *RemoteAccount, error) {
	path, unit := "/api/v1/user/profile", "usd"
	if s.Platform == "newapi" {
		path, unit = "/api/user/self", "quota"
	}
	var user struct {
		ID            int64    `json:"id"`
		Username      string   `json:"username"`
		Email         string   `json:"email"`
		Balance       *float64 `json:"balance"`
		FrozenBalance *float64 `json:"frozen_balance"`
		Quota         *float64 `json:"quota"`
		UsedQuota     *float64 `json:"used_quota"`
	}
	if e := c.data(ctx, s, session, "GET", path, nil, &user); e != nil {
		// Legacy New API sends HTTP 200/success:false for expired management
		// sessions. That envelope has this meaning only on the identity endpoint.
		if errors.Is(e, errConnectorEnvelopeDenied) {
			e = ErrReauth
		}
		return Session{}, nil, e
	}
	if user.ID <= 0 || session.UserID > 0 && session.UserID != user.ID {
		return Session{}, nil, ErrReauth
	}
	balance, frozen, used := user.Balance, user.FrozenBalance, (*float64)(nil)
	if s.Platform == "newapi" {
		balance, frozen, used = user.Quota, nil, user.UsedQuota
	}
	// Settlements can leave an account negative. Frozen and used amounts cannot
	// be negative, and frozen is already excluded from the available balance.
	if balance != nil && (math.IsNaN(*balance) || math.IsInf(*balance, 0)) || !connectorValidRate(frozen) || !connectorValidRate(used) || len(user.Username) > 300 || len(user.Email) > 320 {
		return Session{}, nil, ErrUnsupported
	}
	session.UserID = user.ID
	return session, &RemoteAccount{UserID: user.ID, Username: user.Username, Email: user.Email, Balance: balance, FrozenBalance: frozen, UsedBalance: used, Unit: unit, Source: path}, nil
}
