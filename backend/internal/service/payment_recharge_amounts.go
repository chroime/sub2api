package service

// RechargeAmounts returns the fixed balance recharge denominations allowed by
// the configured range. Each call owns its slice so checkout consumers cannot
// change the denominations accepted by order validation.
func (cfg *PaymentConfig) RechargeAmounts() []float64 {
	presets := [...]float64{10, 20, 50, 100, 200, 500, 1000, 2000, 5000}
	amounts := make([]float64, 0, len(presets))
	for _, amount := range presets {
		if (cfg.MinAmount > 0 && amount < cfg.MinAmount) || (cfg.MaxAmount > 0 && amount > cfg.MaxAmount) {
			continue
		}
		amounts = append(amounts, amount)
	}
	return amounts
}
