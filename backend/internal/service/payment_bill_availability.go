package service

import (
	"context"
	"fmt"
	"sort"
	"strconv"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/paymentproviderinstance"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/Wei-Shaw/sub2api/internal/payment/provider"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// pcAttachBillPayAmounts publishes only exact CNY amounts. QR payloads and
// credentials remain in provider configuration, never in payer-facing limits.
// A selected fixed-QR source without usable rules is not available at checkout.
func (s *PaymentConfigService) pcAttachBillPayAmounts(limits *MethodLimits, instances []*dbent.PaymentProviderInstance) bool {
	if len(instances) == 0 {
		return false
	}
	for _, inst := range instances {
		if inst == nil || inst.ProviderKey != payment.TypeAlipayBill {
			return true
		}
	}
	limits.FixedAmountsOnly = true
	amounts := map[int64]struct{}{}
	for _, inst := range instances {
		rules, err := s.billInstanceRules(inst)
		if err != nil {
			continue
		}
		for _, rule := range rules {
			if rule.Enabled {
				amounts[rule.AmountCents] = struct{}{}
			}
		}
	}
	sorted := make([]int64, 0, len(amounts))
	for cents := range amounts {
		sorted = append(sorted, cents)
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	limits.PayAmounts = make([]string, 0, len(sorted))
	for _, cents := range sorted {
		limits.PayAmounts = append(limits.PayAmounts, fmt.Sprintf("%d.%02d", cents/100, cents%100))
	}
	return len(limits.PayAmounts) > 0
}

func (s *PaymentConfigService) billInstanceRules(inst *dbent.PaymentProviderInstance) ([]provider.AlipayBillRule, error) {
	cfg, err := s.decryptProviderConfig(payment.TypeAlipayBill, inst.Config)
	if err != nil {
		return nil, err
	}
	return provider.ParseAlipayBillRules(cfg)
}

// eligibleBillInstanceIDs narrows the existing load-balancing candidate set by
// the final payable amount, after fees and currency conversion. It does not
// substitute a nearby denomination or change the explicit source selection.
func (s *PaymentConfigService) eligibleBillInstanceIDs(ctx context.Context, paymentType payment.PaymentType, amount float64) ([]int64, error) {
	unavailable := func() error {
		return infraerrors.BadRequest("ALIPAY_BILL_AMOUNT_UNAVAILABLE", "no enabled Alipay fixed QR supports the exact payable amount")
	}
	cents, err := provider.ParseAlipayBillAmountCents(strconv.FormatFloat(amount, 'f', -1, 64))
	if err != nil {
		return nil, unavailable()
	}
	instances, err := s.entClient.PaymentProviderInstance.Query().
		Where(paymentproviderinstance.EnabledEQ(true), paymentproviderinstance.ProviderKeyEQ(payment.TypeAlipayBill)).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("query fixed Alipay payment providers: %w", err)
	}
	ids := make([]int64, 0, len(instances))
	for _, inst := range instances {
		if !payment.InstanceSupportsType(inst.SupportedTypes, paymentType) {
			continue
		}
		rules, err := s.billInstanceRules(inst)
		if err != nil {
			continue
		}
		for _, rule := range rules {
			if rule.Enabled && rule.AmountCents == cents {
				ids = append(ids, inst.ID)
				break
			}
		}
	}
	if len(ids) == 0 {
		return nil, unavailable()
	}
	return ids, nil
}
