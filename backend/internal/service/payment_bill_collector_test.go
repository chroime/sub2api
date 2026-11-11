//go:build unit

package service

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/smartwalle/alipay/v3"
	"github.com/stretchr/testify/require"
)

func TestBillCollectorEligibilityRetainsRecentDisabledGatewayOrders(t *testing.T) {
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name, status string
		enabled      bool
		expires      time.Duration
		want         bool
	}{
		{name: "enabled without gateway history", enabled: true, want: true},
		{name: "disabled without gateway history", want: false},
		{name: "pending payment", status: "PENDING", expires: time.Hour, want: true},
		{name: "pending awaiting expiry sweep", status: "PENDING", expires: -time.Hour, want: true},
		{name: "recent expired payment", status: "EXPIRED", expires: -time.Hour, want: true},
		{name: "at recovery cutoff", status: "EXPIRED", expires: -24 * time.Hour, want: false},
		{name: "old expired payment", status: "EXPIRED", expires: -25 * time.Hour, want: false},
		{name: "old pending payment", status: "PENDING", expires: -25 * time.Hour, want: false},
		{name: "already paid", status: "PAID", expires: time.Hour, want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, id := newBillReceiptTestService(t)
			ctx := context.Background()
			inst, err := s.entClient.PaymentProviderInstance.UpdateOneID(id).SetEnabled(tc.enabled).Save(ctx)
			require.NoError(t, err)
			if tc.status != "" {
				orderID := epayReceiptTestOrder(t, s, id, "COLLECT_ELIGIBILITY")
				_, err = s.entClient.ExecContext(ctx, `UPDATE payment_epay_orders SET status=$1,expires_at=$2 WHERE id=$3`, tc.status, now.Add(tc.expires), orderID)
				require.NoError(t, err)
			}
			eligible, err := s.shouldCollectAlipayBillProvider(ctx, inst, now)
			require.NoError(t, err)
			require.Equal(t, tc.want, eligible)
		})
	}

	t.Run("another provider does not activate a disabled provider", func(t *testing.T) {
		s, id := newBillReceiptTestService(t)
		ctx := context.Background()
		inst, err := s.entClient.PaymentProviderInstance.UpdateOneID(id).SetEnabled(false).Save(ctx)
		require.NoError(t, err)
		other, err := s.entClient.PaymentProviderInstance.Create().SetProviderKey(payment.TypeAlipayBill).SetName("other").SetConfig("{}").Save(ctx)
		require.NoError(t, err)
		epayReceiptTestOrder(t, s, other.ID, "OTHER_PROVIDER")
		eligible, err := s.shouldCollectAlipayBillProvider(ctx, inst, time.Now().UTC())
		require.NoError(t, err)
		require.False(t, eligible)
	})
}

type billCollectorSignedTransport func(*http.Request) (*http.Response, error)

func (f billCollectorSignedTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestBillCollectorRecoversDisabledGatewayReceiptsWithVerifiedUpstream(t *testing.T) {
	// Keep the SDK's request construction and signature verification real while
	// answering its HTTP transport in memory; no network connection is opened.
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	publicDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	require.NoError(t, err)
	privatePEM := string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
	publicPEM := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicDER}))
	for _, tc := range []struct {
		name, status string
		expires      time.Duration
		wantReceipt  bool
		wantMatched  bool
	}{
		{name: "pending confirms and queues callback", status: "PENDING", expires: time.Hour, wantReceipt: true, wantMatched: true},
		{name: "recently expired retains manual recovery", status: "EXPIRED", expires: -time.Hour, wantReceipt: true},
		{name: "old expired history remains idle", status: "EXPIRED", expires: -25 * time.Hour},
		{name: "paid history remains idle", status: "PAID", expires: time.Hour},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			s, id, cfg := newBuiltinEasyPayServiceFixture(t, "https://merchant.example")
			params := builtinEasyPayTestParams(id, "https://merchant.example")
			created, err := s.CreateBuiltinEasyPayOrder(ctx, params)
			require.NoError(t, err)
			order, err := findBuiltinEasyPayOrder(ctx, s.entClient, id, params["out_trade_no"], "")
			require.NoError(t, err)
			now := time.Now().UTC().Truncate(time.Second)
			transAt := now.Add(-2 * time.Hour)
			_, err = s.entClient.ExecContext(ctx, `UPDATE payment_epay_orders SET status=$1,created_at=$2,expires_at=$3 WHERE id=$4`, tc.status, now.Add(-3*time.Hour), now.Add(tc.expires), order.ID)
			require.NoError(t, err)
			cfg["privateKey"], cfg["publicKey"] = privatePEM, publicPEM
			encrypted, err := s.configService.encryptProviderConfig(payment.TypeAlipayBill, cfg)
			require.NoError(t, err)
			_, err = s.entClient.PaymentProviderInstance.UpdateOneID(id).SetConfig(encrypted).Save(ctx)
			require.NoError(t, err)
			disabled := false
			_, err = s.configService.UpdateProviderInstance(ctx, id, UpdateProviderInstanceRequest{Enabled: &disabled})
			require.NoError(t, err, "full disable remains available while old gateway orders recover")

			originalTransport := http.DefaultTransport
			t.Cleanup(func() { http.DefaultTransport = originalTransport })
			http.DefaultTransport = billCollectorSignedTransport(func(r *http.Request) (*http.Response, error) {
				if !tc.wantReceipt {
					t.Error("disabled provider without recent unresolved orders queried upstream")
				}
				require.Equal(t, "openapi.alipay.com", r.URL.Hostname())
				require.NoError(t, r.ParseForm())
				require.Equal(t, "alipay.data.bill.accountlog.query", r.Form.Get("method"))
				var request map[string]string
				require.NoError(t, json.Unmarshal([]byte(r.Form.Get("biz_content")), &request))
				require.Equal(t, "2088000000000001", request["bill_user_id"])
				location := time.FixedZone("Asia/Shanghai", 8*60*60)
				start, err := time.ParseInLocation("2006-01-02 15:04:05", request["start_time"], location)
				require.NoError(t, err)
				end, err := time.ParseInLocation("2006-01-02 15:04:05", request["end_time"], location)
				require.NoError(t, err)
				response := alipay.BillAccountLogQueryRsp{Error: alipay.Error{Code: alipay.CodeSuccess}, PageNo: request["page_no"], PageSize: "1000", TotalSize: "0", DetailList: []*alipay.AccountLogItem{}}
				if !transAt.Before(start) && transAt.Before(end) {
					response.TotalSize = "1"
					response.DetailList = []*alipay.AccountLogItem{{AccountLogId: "DISABLED_GATEWAY_LOG", AliPayOrderNo: "DISABLED_GATEWAY_ALI", MerchantOrderNo: created.TradeNo, TransAmount: "100.00", Direction: "收入", TransDt: transAt.In(location).Format("2006-01-02 15:04:05")}}
				}
				body, err := json.Marshal(response)
				require.NoError(t, err)
				digest := sha256.Sum256(body)
				signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
				require.NoError(t, err)
				payload := fmt.Sprintf(`{"alipay_data_bill_accountlog_query_response":%s,"sign":%q}`, body, base64.StdEncoding.EncodeToString(signature))
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(payload)), Request: r}, nil
			})

			require.NoError(t, s.CollectAlipayBillReceipts(ctx))
			page, err := s.ListAlipayBillReceipts(ctx, id, 1, 20, "")
			require.NoError(t, err)
			if !tc.wantReceipt {
				require.Empty(t, page.Items)
				return
			}
			require.Len(t, page.Items, 1)
			if tc.wantMatched {
				require.Equal(t, "matched", page.Items[0].Status)
			} else {
				require.Equal(t, "review", page.Items[0].Status)
				require.NoError(t, s.ClaimBuiltinEasyPayReceipt(ctx, id, page.Items[0].ID, order.ID, "admin:1", "merchant verified delayed receipt"))
			}
			paid, err := getBuiltinEasyPayOrderForReceipt(ctx, s.entClient, order.ID)
			require.NoError(t, err)
			require.Equal(t, "PAID", paid.Status)
			require.Equal(t, "pending", paid.NotifyStatus)
			require.Equal(t, "DISABLED_GATEWAY_ALI", paid.AlipayOrderNo)
			inst, err := s.entClient.PaymentProviderInstance.Get(ctx, id)
			require.NoError(t, err)
			require.False(t, inst.Enabled, "receipt recovery must not re-enable new payments")
		})
	}
}
