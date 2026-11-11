//go:build unit

package provider

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/payment"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

func alipayBillTestConfig() map[string]string {
	return map[string]string{
		"appId": "2021001234567890", "privateKey": "private-key-placeholder",
		"publicKey": "public-key-placeholder", "receiverId": "2088123456789012",
		"qrRules": `[{"id":"ten","amount":"10.00","qr_url":"https://qr.alipay.com/ten","enabled":true,"sort_order":2},{"id":"disabled","amount":"10.01","qr_url":"https://qr.alipay.com/disabled","enabled":false,"sort_order":0},{"id":"exact","amount":"10.01","qr_url":"https://qr.alipay.com/exact","enabled":true,"sort_order":1}]`,
	}
}

func TestAlipayBillCreatePaymentSelectsExactEnabledAmount(t *testing.T) {
	p, err := CreateProvider("alipay_bill", "bill-instance", alipayBillTestConfig())
	if err != nil {
		t.Fatalf("create fixed-QR provider: %v", err)
	}
	result, err := p.CreatePayment(context.Background(), payment.CreatePaymentRequest{OrderID: "internal-order", Amount: "10.01", PaymentType: payment.TypeAlipay})
	if err != nil {
		t.Fatalf("create exact amount: %v", err)
	}
	if result.QRCode != "https://qr.alipay.com/exact" || result.PayURL != result.QRCode || result.QRRuleID != "exact" {
		t.Fatalf("selected payment payload = %#v", result)
	}
	if result.TradeNo != "" || result.Currency != "CNY" {
		t.Fatalf("static QR must not claim an upstream trade: %#v", result)
	}
	for _, amount := range []string{"10.02", "9.99"} {
		if _, err := p.CreatePayment(context.Background(), payment.CreatePaymentRequest{Amount: amount}); infraerrors.Reason(err) != "ALIPAY_BILL_QR_NO_MATCH" {
			t.Errorf("unconfigured amount %s: got %v", amount, err)
		}
	}
	for _, amount := range []string{"", "0", "-1", "+1", "1e1", "NaN", "10.010", " 10.01", "10.", ".01", "1000000.01"} {
		if _, err := p.CreatePayment(context.Background(), payment.CreatePaymentRequest{Amount: amount}); infraerrors.Reason(err) != "ALIPAY_BILL_INVALID_AMOUNT" {
			t.Errorf("invalid amount %q: got %v", amount, err)
		}
	}
}

func TestParseAlipayBillAmountCentsUsesExactIntegerCents(t *testing.T) {
	for _, tt := range []struct {
		amount string
		cents  int64
	}{
		{"0.01", 1}, {"10", 1000}, {"10.1", 1010}, {"999999.99", 99999999}, {"1000000.00", 100000000},
	} {
		got, err := ParseAlipayBillAmountCents(tt.amount)
		if err != nil || got != tt.cents {
			t.Errorf("ParseAlipayBillAmountCents(%q) = %d, %v; want %d", tt.amount, got, err, tt.cents)
		}
	}
}

func TestAlipayBillConstructorRejectsInvalidConfigWithoutExposingValues(t *testing.T) {
	for _, key := range []string{"appId", "privateKey", "publicKey", "receiverId"} {
		t.Run("missing_"+key, func(t *testing.T) {
			config := alipayBillTestConfig()
			config[key] = " \t "
			_, err := CreateProvider("alipay_bill", "bill-instance", config)
			if infraerrors.Reason(err) != "ALIPAY_BILL_CONFIG_MISSING_KEY" {
				t.Fatalf("missing %s: %v", key, err)
			}
			if strings.Contains(err.Error(), "placeholder") || strings.Contains(err.Error(), "https://") {
				t.Fatalf("config error exposes a sensitive value: %v", err)
			}
		})
	}
	for _, recipient := range []string{"user@example.com", "208812345678901", "20881234567890123", "3088123456789012"} {
		config := alipayBillTestConfig()
		config["receiverId"] = recipient
		if _, err := CreateProvider("alipay_bill", "bill-instance", config); infraerrors.Reason(err) != "ALIPAY_BILL_INVALID_RECEIVER" {
			t.Errorf("invalid receiver %q: %v", recipient, err)
		}
	}
}

func TestAlipayBillConstructorRejectsInvalidRules(t *testing.T) {
	tests := []struct{ name, rules string }{
		{"not_array", `{}`},
		{"null", `null`},
		{"trailing_data", `[] []`},
		{"unknown_field", `[{"id":"one","amount":"1","qr_url":"https://qr.alipay.com/one","enabled":true,"url":"secret"}]`},
		{"missing_id", `[{"amount":"1","qr_url":"https://qr.alipay.com/one","enabled":true}]`},
		{"missing_amount", `[{"id":"one","qr_url":"https://qr.alipay.com/one","enabled":true}]`},
		{"duplicate_id", `[{"id":"one","amount":"1","qr_url":"https://qr.alipay.com/one","enabled":true},{"id":"one","amount":"2","qr_url":"https://qr.alipay.com/two","enabled":false}]`},
		{"duplicate_enabled_amount", `[{"id":"one","amount":"1","qr_url":"https://qr.alipay.com/one","enabled":true},{"id":"two","amount":"1.00","qr_url":"https://qr.alipay.com/two","enabled":true}]`},
		{"zero_amount", `[{"id":"one","amount":"0.00","qr_url":"https://qr.alipay.com/one","enabled":true}]`},
		{"excess_precision", `[{"id":"one","amount":"1.001","qr_url":"https://qr.alipay.com/one","enabled":true}]`},
	}
	for _, qr := range []string{"", "http://qr.alipay.com/one", "https://qr.alipay.com", "https://qr.alipay.com/", "https://qr.alipay.com.evil.example/one", "https://user:secret@qr.alipay.com/one", "https://qr.alipay.com/one#secret", "https://qr.alipay.com:443/one", "https://example.com/one"} {
		rule, err := json.Marshal([]map[string]any{{"id": "one", "amount": "1", "qr_url": qr, "enabled": true}})
		if err != nil {
			t.Fatal(err)
		}
		tests = append(tests, struct{ name, rules string }{"url_" + qr, string(rule)})
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := alipayBillTestConfig()
			config["qrRules"] = tt.rules
			_, err := CreateProvider("alipay_bill", "bill-instance", config)
			if infraerrors.Reason(err) != "ALIPAY_BILL_INVALID_RULES" {
				t.Fatalf("invalid rules accepted or wrong error: %v", err)
			}
			if strings.Contains(err.Error(), "https://") || strings.Contains(err.Error(), "secret") {
				t.Fatalf("rule error exposes QR data: %v", err)
			}
		})
	}
}

func TestAlipayBillQueryOnlyAndUnsupportedOperations(t *testing.T) {
	config := alipayBillTestConfig()
	config["qrRules"] = "[]"
	p, err := CreateProvider("alipay_bill", "bill-instance", config)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.CreatePayment(context.Background(), payment.CreatePaymentRequest{Amount: "10.00"}); infraerrors.Reason(err) != "ALIPAY_BILL_QR_NO_MATCH" {
		t.Fatalf("query-only provider offered payment: %v", err)
	}
	status, err := p.QueryOrder(context.Background(), "unknown-order")
	if err != nil || status.Status != payment.ProviderStatusPending || status.Amount != 0 {
		t.Fatalf("query synthesized payment evidence: %#v, %v", status, err)
	}
	metadata := p.(payment.MerchantIdentityProvider).MerchantIdentityMetadata()
	if len(metadata) != 2 || metadata["app_id"] != "2021001234567890" || metadata["receiver_id"] != "2088123456789012" {
		t.Fatalf("merchant identity = %#v", metadata)
	}
	if _, err := p.Refund(context.Background(), payment.RefundRequest{}); infraerrors.Reason(err) != "REFUND_UNSUPPORTED" {
		t.Fatalf("static QR refund must be unsupported: %v", err)
	}
	if _, err := p.VerifyNotification(context.Background(), "forged", nil); infraerrors.Reason(err) != "ALIPAY_BILL_NOTIFICATION_UNSUPPORTED" {
		t.Fatalf("static QR callback must be unsupported: %v", err)
	}
}

func TestAlipayBillQueryClientAllowsMissingReceiverButCannotCreatePayment(t *testing.T) {
	config := alipayBillTestConfig()
	delete(config, "receiverId")
	queryClient, err := NewAlipayBillQueryClient("bill-query-only", config)
	if err != nil {
		t.Fatalf("read-only query should not require a receiving account ID: %v", err)
	}
	result, err := queryClient.CreatePayment(context.Background(), payment.CreatePaymentRequest{Amount: "10.00"})
	if result != nil || infraerrors.Reason(err) != "ALIPAY_BILL_CONFIG_MISSING_KEY" || infraerrors.FromError(err).Metadata["key"] != "receiverId" {
		t.Fatalf("query-only client presented a configured QR without a receiving identity: %#v, %v", result, err)
	}
	if _, err := NewAlipayBill("bill-payment", config); infraerrors.Reason(err) != "ALIPAY_BILL_CONFIG_MISSING_KEY" {
		t.Fatalf("payment provider accepted an unspecified receiving identity: %v", err)
	}
	metadata := queryClient.MerchantIdentityMetadata()
	if len(metadata) != 1 || metadata["app_id"] != config["appId"] {
		t.Fatalf("unconfigured receiver was represented as verified merchant identity: %#v", metadata)
	}
}

func TestAlipayBillQueryClientStillValidatesCredentialsAndProvidedReceiver(t *testing.T) {
	for _, key := range []string{"appId", "privateKey", "publicKey"} {
		t.Run(key, func(t *testing.T) {
			config := alipayBillTestConfig()
			delete(config, "receiverId")
			config[key] = " \t "
			if _, err := NewAlipayBillQueryClient("bill-query-only", config); infraerrors.Reason(err) != "ALIPAY_BILL_CONFIG_MISSING_KEY" || infraerrors.FromError(err).Metadata["key"] != key {
				t.Fatalf("missing query credential %s was accepted: %v", key, err)
			}
		})
	}
	config := alipayBillTestConfig()
	config["receiverId"] = "not-an-alipay-uid"
	if _, err := NewAlipayBillQueryClient("bill-query-only", config); infraerrors.Reason(err) != "ALIPAY_BILL_INVALID_RECEIVER" {
		t.Fatalf("malformed explicit receiver silently became default-account query: %v", err)
	}
}
