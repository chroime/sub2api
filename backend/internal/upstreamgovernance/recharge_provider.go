package upstreamgovernance

import "context"

// RechargeProvider is a future integration boundary, not a registered runtime
// capability. Implementations must support lookup by the same durable key used
// for Submit; timeout/unknown outcomes must be reconciled before another order.
// All amounts are minor currency units, never native wallet quota or float64.
type RechargeProvider interface {
	Capability(context.Context, RechargeWallet) (RechargeCapability, error)
	Submit(context.Context, RechargeOrderRequest) (RechargeOrderResult, error)
	Lookup(context.Context, RechargeOrderQuery) (RechargeOrderResult, error)
}

type RechargeWallet struct {
	SiteID            int64
	BaseURL, Platform string
	UserID            int64
}

type RechargeOrderRequest struct {
	Wallet         RechargeWallet
	IdempotencyKey string
	AmountMinor    int64
	Currency       string
}

type RechargeOrderQuery struct {
	Wallet                          RechargeWallet
	IdempotencyKey, ExternalOrderID string
}

type RechargeOrderResult struct {
	ExternalOrderID string
	// pending, succeeded, failed, or unknown. Only a definitive pre-charge
	// failure releases a reservation; unknown remains charged against budgets.
	State        string
	ChargedMinor int64
	Currency     string
}
