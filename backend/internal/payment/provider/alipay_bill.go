package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/Wei-Shaw/sub2api/internal/payment"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/smartwalle/alipay/v3"
)

const (
	alipayBillMaxAmountCents = int64(100_000_000) // CNY 1,000,000.00
	alipayBillMaxRules       = 1000
	alipayBillMaxRulesBytes  = 256 * 1024
)

var (
	alipayBillAmountPattern   = regexp.MustCompile(`^(0|[1-9][0-9]*)(\.[0-9]{1,2})?$`)
	alipayBillReceiverPattern = regexp.MustCompile(`^2088[0-9]{12}$`)
	alipayBillRuleIDPattern   = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
)

// AlipayBillRule binds a reusable Alipay QR to one exact CNY payment amount.
// AmountCents is derived from Amount and is never accepted from configuration.
type AlipayBillRule struct {
	ID          string `json:"id"`
	Amount      string `json:"amount"`
	QRURL       string `json:"qr_url"`
	Enabled     bool   `json:"enabled"`
	SortOrder   int    `json:"sort_order"`
	AmountCents int64  `json:"-"`
}

// ParseAlipayBillAmountCents parses a positive CNY amount without floating point
// rounding, exponents, signs, whitespace, or more than two decimal places.
func ParseAlipayBillAmountCents(amount string) (int64, error) {
	invalid := func() (int64, error) {
		return 0, infraerrors.BadRequest("ALIPAY_BILL_INVALID_AMOUNT", "amount must be a positive CNY decimal with at most two decimal places, up to 1000000.00")
	}
	if len(amount) > 10 || !alipayBillAmountPattern.MatchString(amount) {
		return invalid()
	}
	parts := strings.SplitN(amount, ".", 2)
	whole, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return invalid()
	}
	cents := whole * 100
	if len(parts) == 2 {
		fraction := parts[1]
		if len(fraction) == 1 {
			fraction += "0"
		}
		fractionCents, err := strconv.ParseInt(fraction, 10, 64)
		if err != nil {
			return invalid()
		}
		cents += fractionCents
	}
	if cents <= 0 || cents > alipayBillMaxAmountCents {
		return invalid()
	}
	return cents, nil
}

// ParseAlipayBillRules validates configuration without echoing QR payloads in
// errors. Empty rules permit connectivity checks before configuring checkout.
func ParseAlipayBillRules(config map[string]string) ([]AlipayBillRule, error) {
	raw := strings.TrimSpace(config["qrRules"])
	if raw == "" {
		return []AlipayBillRule{}, nil
	}
	invalid := func(message string) ([]AlipayBillRule, error) {
		return nil, infraerrors.BadRequest("ALIPAY_BILL_INVALID_RULES", message)
	}
	if len(raw) > alipayBillMaxRulesBytes {
		return invalid("QR rules exceed the configuration size limit")
	}
	var rules []AlipayBillRule
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&rules); err != nil || rules == nil {
		return invalid("QR rules must be a JSON array of valid rule objects")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return invalid("QR rules must contain only one JSON array")
	}
	if len(rules) > alipayBillMaxRules {
		return invalid("too many QR rules")
	}
	ids := make(map[string]struct{}, len(rules))
	enabledAmounts := make(map[int64]struct{}, len(rules))
	for i := range rules {
		rule := &rules[i]
		if !alipayBillRuleIDPattern.MatchString(rule.ID) {
			return invalid("each QR rule needs a unique ID of 1 to 64 letters, digits, underscores, or hyphens")
		}
		if _, exists := ids[rule.ID]; exists {
			return invalid("QR rule IDs must be unique")
		}
		ids[rule.ID] = struct{}{}
		cents, err := ParseAlipayBillAmountCents(rule.Amount)
		if err != nil {
			return invalid("each QR rule needs a positive CNY amount with at most two decimal places, up to 1000000.00")
		}
		rule.AmountCents = cents
		if !validAlipayBillQRURL(rule.QRURL) {
			return invalid("each QR rule needs an HTTPS qr.alipay.com URL with a payment path and no credentials, port, or fragment")
		}
		if rule.Enabled {
			if _, exists := enabledAmounts[cents]; exists {
				return invalid("enabled QR rules must have distinct exact payment amounts")
			}
			enabledAmounts[cents] = struct{}{}
		}
	}
	sort.SliceStable(rules, func(i, j int) bool { return rules[i].SortOrder < rules[j].SortOrder })
	return rules, nil
}

func validAlipayBillQRURL(raw string) bool {
	if raw == "" || len(raw) > 2048 || strings.TrimSpace(raw) != raw || strings.Contains(raw, "#") {
		return false
	}
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Host == "qr.alipay.com" &&
		u.User == nil && u.Opaque == "" && u.Fragment == "" && strings.Trim(u.Path, "/") != ""
}

// AlipayBill presents configured fixed-amount QR links and separately reads
// official account logs. A reusable QR does not create a queryable Alipay trade.
type AlipayBill struct {
	instanceID string
	config     map[string]string
	rules      []AlipayBillRule

	mu     sync.Mutex
	client *alipay.Client
}

func NewAlipayBill(instanceID string, config map[string]string) (*AlipayBill, error) {
	return newAlipayBill(instanceID, config, true)
}

// NewAlipayBillQueryClient permits read-only connection checks using the app's
// default authorized account before the receiving account ID is configured.
// Checkout still requires an explicit receiving identity.
func NewAlipayBillQueryClient(instanceID string, config map[string]string) (*AlipayBill, error) {
	return newAlipayBill(instanceID, config, false)
}

func newAlipayBill(instanceID string, config map[string]string, requireReceiver bool) (*AlipayBill, error) {
	copyConfig := make(map[string]string, len(config))
	for key, value := range config {
		copyConfig[key] = value
	}
	required := []string{"appId", "privateKey", "publicKey"}
	if requireReceiver {
		required = append(required, "receiverId")
	}
	for _, key := range required {
		copyConfig[key] = strings.TrimSpace(copyConfig[key])
		if copyConfig[key] == "" {
			return nil, infraerrors.BadRequest("ALIPAY_BILL_CONFIG_MISSING_KEY", "missing required Alipay bill configuration key").
				WithMetadata(map[string]string{"key": key})
		}
	}
	copyConfig["receiverId"] = strings.TrimSpace(copyConfig["receiverId"])
	copyConfig["appAuthToken"] = strings.TrimSpace(copyConfig["appAuthToken"])
	if copyConfig["receiverId"] != "" && !alipayBillReceiverPattern.MatchString(copyConfig["receiverId"]) {
		return nil, infraerrors.BadRequest("ALIPAY_BILL_INVALID_RECEIVER", "receiverId must be the 16-digit Alipay user ID beginning with 2088")
	}
	rules, err := ParseAlipayBillRules(copyConfig)
	if err != nil {
		return nil, err
	}
	return &AlipayBill{instanceID: instanceID, config: copyConfig, rules: rules}, nil
}

func (a *AlipayBill) Name() string        { return "Alipay Fixed QR" }
func (a *AlipayBill) ProviderKey() string { return payment.TypeAlipayBill }
func (a *AlipayBill) SupportedTypes() []payment.PaymentType {
	return []payment.PaymentType{payment.TypeAlipay}
}

func (a *AlipayBill) MerchantIdentityMetadata() map[string]string {
	if a == nil {
		return nil
	}
	metadata := map[string]string{"app_id": a.config["appId"]}
	if receiver := a.config["receiverId"]; receiver != "" {
		metadata["receiver_id"] = receiver
	}
	return metadata
}

func (a *AlipayBill) CreatePayment(_ context.Context, req payment.CreatePaymentRequest) (*payment.CreatePaymentResponse, error) {
	if a.config["receiverId"] == "" {
		return nil, infraerrors.BadRequest("ALIPAY_BILL_CONFIG_MISSING_KEY", "missing required Alipay bill configuration key").
			WithMetadata(map[string]string{"key": "receiverId"})
	}
	cents, err := ParseAlipayBillAmountCents(req.Amount)
	if err != nil {
		return nil, err
	}
	for _, rule := range a.rules {
		if rule.Enabled && rule.AmountCents == cents {
			return &payment.CreatePaymentResponse{
				QRCode: rule.QRURL, PayURL: rule.QRURL, QRRuleID: rule.ID,
				Currency: "CNY", ResultType: payment.CreatePaymentResultOrderCreated,
			}, nil
		}
	}
	return nil, infraerrors.BadRequest("ALIPAY_BILL_QR_NO_MATCH", "no enabled Alipay QR rule matches the exact payable amount")
}

func (a *AlipayBill) QueryOrder(_ context.Context, _ string) (*payment.QueryOrderResponse, error) {
	// Only verified statement reconciliation may settle a fixed-QR payment.
	return &payment.QueryOrderResponse{Status: payment.ProviderStatusPending, Metadata: a.MerchantIdentityMetadata()}, nil
}

func (a *AlipayBill) VerifyNotification(_ context.Context, _ string, _ map[string]string) (*payment.PaymentNotification, error) {
	return nil, infraerrors.BadRequest("ALIPAY_BILL_NOTIFICATION_UNSUPPORTED", "fixed Alipay QR payments do not support payment notifications")
}

func (a *AlipayBill) Refund(_ context.Context, _ payment.RefundRequest) (*payment.RefundResponse, error) {
	return nil, infraerrors.BadRequest("REFUND_UNSUPPORTED", "fixed Alipay QR payments require manual refunds")
}

var (
	_ payment.Provider                 = (*AlipayBill)(nil)
	_ payment.MerchantIdentityProvider = (*AlipayBill)(nil)
)
