package handler

import "github.com/Wei-Shaw/sub2api/internal/service"

// Keep the customer-facing configuration explicit so new administrative
// payment settings cannot silently become part of the user API contract.
type userPaymentConfigResponse struct {
	Enabled                       bool                        `json:"enabled"`
	MinAmount                     float64                     `json:"min_amount"`
	MaxAmount                     float64                     `json:"max_amount"`
	DailyLimit                    float64                     `json:"daily_limit"`
	OrderTimeoutMin               int                         `json:"order_timeout_minutes"`
	MaxPendingOrders              int                         `json:"max_pending_orders"`
	EnabledTypes                  []string                    `json:"enabled_payment_types"`
	BalanceDisabled               bool                        `json:"balance_disabled"`
	BalanceRechargeMultiplier     float64                     `json:"balance_recharge_multiplier"`
	SubscriptionUSDToCNYRate      float64                     `json:"subscription_usd_to_cny_rate"`
	RechargeFeeRate               float64                     `json:"recharge_fee_rate"`
	RechargeBonusTiers            []service.RechargeBonusTier `json:"recharge_bonus_tiers"`
	RechargeBonusMode             string                      `json:"recharge_bonus_mode"`
	RechargeBonusNotice           string                      `json:"recharge_bonus_notice"`
	LoadBalanceStrategy           string                      `json:"load_balance_strategy"`
	ProductNamePrefix             string                      `json:"product_name_prefix"`
	ProductNameSuffix             string                      `json:"product_name_suffix"`
	HelpImageURL                  string                      `json:"help_image_url"`
	HelpText                      string                      `json:"help_text"`
	StripePublishableKey          string                      `json:"stripe_publishable_key,omitempty"`
	CancelRateLimitEnabled        bool                        `json:"cancel_rate_limit_enabled"`
	CancelRateLimitMax            int                         `json:"cancel_rate_limit_max"`
	CancelRateLimitWindow         int                         `json:"cancel_rate_limit_window"`
	CancelRateLimitUnit           string                      `json:"cancel_rate_limit_unit"`
	CancelRateLimitMode           string                      `json:"cancel_rate_limit_window_mode"`
	AlipayForceQRCode             bool                        `json:"alipay_force_qrcode"`
	AlipayMobilePrecreateDeepLink bool                        `json:"alipay_mobile_precreate_deep_link"`
}

func paymentConfigForUser(cfg *service.PaymentConfig) userPaymentConfigResponse {
	return userPaymentConfigResponse{
		Enabled:                       cfg.Enabled,
		MinAmount:                     cfg.MinAmount,
		MaxAmount:                     cfg.MaxAmount,
		DailyLimit:                    cfg.DailyLimit,
		OrderTimeoutMin:               cfg.OrderTimeoutMin,
		MaxPendingOrders:              cfg.MaxPendingOrders,
		EnabledTypes:                  cfg.EnabledTypes,
		BalanceDisabled:               cfg.BalanceDisabled,
		BalanceRechargeMultiplier:     cfg.BalanceRechargeMultiplier,
		SubscriptionUSDToCNYRate:      cfg.SubscriptionUSDToCNYRate,
		RechargeFeeRate:               cfg.RechargeFeeRate,
		RechargeBonusTiers:            cfg.RechargeBonusTiers,
		RechargeBonusMode:             cfg.RechargeBonusMode,
		RechargeBonusNotice:           cfg.RechargeBonusNotice,
		LoadBalanceStrategy:           cfg.LoadBalanceStrategy,
		ProductNamePrefix:             cfg.ProductNamePrefix,
		ProductNameSuffix:             cfg.ProductNameSuffix,
		HelpImageURL:                  cfg.HelpImageURL,
		HelpText:                      cfg.HelpText,
		StripePublishableKey:          cfg.StripePublishableKey,
		CancelRateLimitEnabled:        cfg.CancelRateLimitEnabled,
		CancelRateLimitMax:            cfg.CancelRateLimitMax,
		CancelRateLimitWindow:         cfg.CancelRateLimitWindow,
		CancelRateLimitUnit:           cfg.CancelRateLimitUnit,
		CancelRateLimitMode:           cfg.CancelRateLimitMode,
		AlipayForceQRCode:             cfg.AlipayForceQRCode,
		AlipayMobilePrecreateDeepLink: cfg.AlipayMobilePrecreateDeepLink,
	}
}
