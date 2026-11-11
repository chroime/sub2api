//go:build unit

package provider

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
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/smartwalle/alipay/v3"
)

type alipayBillQuerier interface {
	QueryBills(context.Context, time.Time, time.Time, int) (*AlipayBillPage, error)
}

var alipayBillTestKeys struct {
	once                  sync.Once
	key                   *rsa.PrivateKey
	privatePEM, publicPEM string
	err                   error
}

func alipayBillSigningKeys(t *testing.T) (*rsa.PrivateKey, string, string) {
	t.Helper()
	alipayBillTestKeys.once.Do(func() {
		key, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			alipayBillTestKeys.err = err
			return
		}
		publicDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
		if err != nil {
			alipayBillTestKeys.err = err
			return
		}
		alipayBillTestKeys.key = key
		alipayBillTestKeys.privatePEM = string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
		alipayBillTestKeys.publicPEM = string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicDER}))
	})
	if alipayBillTestKeys.err != nil {
		t.Fatal(alipayBillTestKeys.err)
	}
	return alipayBillTestKeys.key, alipayBillTestKeys.privatePEM, alipayBillTestKeys.publicPEM
}

func alipayBillQueryProvider(t *testing.T, handler http.HandlerFunc) alipayBillQuerier {
	t.Helper()
	_, privatePEM, publicPEM := alipayBillSigningKeys(t)
	config := alipayBillTestConfig()
	config["privateKey"], config["publicKey"], config["appAuthToken"] = privatePEM, publicPEM, "authorization-secret"
	p, err := NewAlipayBill("bill-query", config)
	if err != nil {
		t.Fatal(err)
	}
	if handler != nil {
		server := httptest.NewServer(handler)
		t.Cleanup(server.Close)
		p.client, err = alipay.New(config["appId"], privatePEM, true, alipay.WithProductionGateway(server.URL), alipay.WithHTTPClient(server.Client()))
		if err != nil {
			t.Fatal(err)
		}
		if err := p.client.LoadAliPayPublicKey(publicPEM); err != nil {
			t.Fatal(err)
		}
	}
	querier, ok := any(p).(alipayBillQuerier)
	if !ok {
		t.Fatal("fixed QR provider is missing official bill query capability")
	}
	return querier
}

func alipayBillWriteSignedResponse(t *testing.T, w http.ResponseWriter, response any) {
	t.Helper()
	key, _, _ := alipayBillSigningKeys(t)
	body, err := json.Marshal(response)
	if err != nil {
		t.Error(err)
		return
	}
	digest := sha256.Sum256(body)
	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		t.Error(err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"alipay_data_bill_accountlog_query_response":%s,"sign":%q}`, body, base64.StdEncoding.EncodeToString(signature))
}

func TestAlipayBillQueryUsesReceiverPaginationAndShanghaiTime(t *testing.T) {
	p := alipayBillQueryProvider(t, func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Error(err)
			return
		}
		if r.Form.Get("method") != "alipay.data.bill.accountlog.query" || r.Form.Get("app_auth_token") != "authorization-secret" {
			t.Errorf("query method or delegated token was not forwarded")
		}
		var request map[string]string
		if err := json.Unmarshal([]byte(r.Form.Get("biz_content")), &request); err != nil {
			t.Error(err)
			return
		}
		for key, want := range map[string]string{"start_time": "2026-11-10 08:00:00", "end_time": "2026-11-10 09:00:00", "page_no": "2", "page_size": "1000", "bill_user_id": "2088123456789012"} {
			if request[key] != want {
				t.Errorf("query %s = %q, want %q", key, request[key], want)
			}
		}
		if request["merchant_order_no"] != "" || request["alipay_order_no"] != "" {
			t.Errorf("query unexpectedly filtered by an order number")
		}
		alipayBillWriteSignedResponse(t, w, alipay.BillAccountLogQueryRsp{
			Error: alipay.Error{Code: alipay.CodeSuccess}, PageNo: "2", PageSize: "1000", TotalSize: "1002",
			DetailList: []*alipay.AccountLogItem{
				{AccountLogId: "log-1", AliPayOrderNo: "trade-1", MerchantOrderNo: "sub2api-order-1", TransAmount: "10.01", Direction: "收入", TransDt: "2026-11-10 08:30:00", BillSource: "商家中心", TransMemo: "must never become order identity"},
				{AccountLogId: "log-2", AliPayOrderNo: "trade-2", TransAmount: "-1.50", Direction: "支出", TransDt: "2026-11-10 08:31:00", BillSource: "商家中心"},
			},
		})
	})
	start := time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC)
	page, err := p.QueryBills(context.Background(), start, start.Add(time.Hour), 2)
	if err != nil {
		t.Fatal(err)
	}
	if page.Page != 2 || page.Total != 1002 || len(page.Items) != 2 {
		t.Fatalf("bill pagination lost: %#v", page)
	}
	entry := page.Items[0]
	if entry.AccountLogID != "log-1" || entry.AlipayOrderNo != "trade-1" || entry.MerchantOrderNo != "sub2api-order-1" || entry.Amount != "10.01" || entry.Direction != "收入" || entry.BillSource != "商家中心" {
		t.Fatalf("receipt identity changed: %#v", entry)
	}
	if entry.TransAt.Format(time.RFC3339) != "2026-11-10T08:30:00+08:00" {
		t.Fatalf("account log timestamp = %s", entry.TransAt.Format(time.RFC3339))
	}
	if page.Items[1].Amount != "-1.50" || page.Items[1].Direction != "支出" || page.Items[1].MerchantOrderNo != "" {
		t.Fatalf("debit became a receipt or acquired order identity: %#v", page.Items[1])
	}
}

func TestAlipayBillQueryRejectsInvalidBoundsBeforeUpstream(t *testing.T) {
	p := alipayBillQueryProvider(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("invalid query reached upstream")
		w.WriteHeader(http.StatusInternalServerError)
	})
	start := time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC)
	for _, tt := range []struct {
		name       string
		start, end time.Time
		page       int
	}{
		{"zero_start", time.Time{}, start, 1},
		{"reversed", start, start.Add(-time.Second), 1},
		{"empty", start, start, 1},
		{"excess_range", start, start.Add(31*24*time.Hour + time.Second), 1},
		{"zero_page", start, start.Add(time.Hour), 0},
		{"negative_page", start, start.Add(time.Hour), -1},
		{"excess_page", start, start.Add(time.Hour), 1001},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := p.QueryBills(context.Background(), tt.start, tt.end, tt.page); infraerrors.Reason(err) != "ALIPAY_BILL_INVALID_QUERY" {
				t.Fatalf("query bounds error = %v", err)
			}
		})
	}
}

func TestAlipayBillQuerySanitizesBusinessAndTransportFailures(t *testing.T) {
	for _, tt := range []struct {
		name        string
		response    any
		raw         string
		wantSubCode string
	}{
		{"business_error", alipay.BillAccountLogQueryRsp{Error: alipay.Error{Code: alipay.CodeBusinessFailed, SubCode: "ACQ.ACCESS_FORBIDDEN", Msg: "upstream-secret", SubMsg: "https://secret.example/authorization-secret"}}, "", "ACQ.ACCESS_FORBIDDEN"},
		{"unsafe_code", alipay.BillAccountLogQueryRsp{Error: alipay.Error{Code: "authorization-secret", SubCode: "https://secret.example"}}, "", ""},
		{"unexpected_pending_code", alipay.BillAccountLogQueryRsp{Error: alipay.Error{Code: alipay.CodeOrderSuccessPayInProcess}}, "", ""},
		{"unsigned_success", nil, `{"alipay_data_bill_accountlog_query_response":{"code":"10000","page_no":"1","total_size":"0","detail_list":[]}}`, ""},
		{"invalid_signature", nil, `{"alipay_data_bill_accountlog_query_response":{"code":"10000","page_no":"1","total_size":"0","detail_list":[]},"sign":"upstream-secret"}`, ""},
		{"not_json", nil, `upstream-secret: authorization-secret`, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p := alipayBillQueryProvider(t, func(w http.ResponseWriter, r *http.Request) {
				if tt.response != nil {
					alipayBillWriteSignedResponse(t, w, tt.response)
				} else {
					fmt.Fprint(w, tt.raw)
				}
			})
			start := time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC)
			result, err := p.QueryBills(context.Background(), start, start.Add(time.Hour), 1)
			if result != nil || infraerrors.Reason(err) != "ALIPAY_BILL_QUERY_FAILED" {
				t.Fatalf("upstream failure became success: result=%#v, err=%v", result, err)
			}
			if strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "https://") {
				t.Fatalf("error leaks upstream data: %v", err)
			}
			if infraerrors.FromError(err).Metadata["sub_code"] != tt.wantSubCode {
				t.Fatalf("unexpected safe subcode metadata: %#v", infraerrors.FromError(err).Metadata)
			}
		})
	}
}

func TestAlipayBillQueryRejectsMalformedSuccessfulResponse(t *testing.T) {
	for _, tt := range []struct {
		name     string
		response alipay.BillAccountLogQueryRsp
	}{
		{"missing_pagination", alipay.BillAccountLogQueryRsp{}},
		{"wrong_page", alipay.BillAccountLogQueryRsp{PageNo: "2", TotalSize: "0"}},
		{"wrong_page_size", alipay.BillAccountLogQueryRsp{PageNo: "1", PageSize: "2000", TotalSize: "0"}},
		{"invalid_total", alipay.BillAccountLogQueryRsp{PageNo: "1", TotalSize: "-1"}},
		{"missing_log_id", alipay.BillAccountLogQueryRsp{PageNo: "1", TotalSize: "1", DetailList: []*alipay.AccountLogItem{{TransAmount: "1.00", Direction: "收入", TransDt: "2026-11-10 08:30:00"}}}},
		{"invalid_timestamp", alipay.BillAccountLogQueryRsp{PageNo: "1", TotalSize: "1", DetailList: []*alipay.AccountLogItem{{AccountLogId: "log-1", TransAmount: "1.00", Direction: "收入", TransDt: "unknown"}}}},
		{"invalid_amount", alipay.BillAccountLogQueryRsp{PageNo: "1", TotalSize: "1", DetailList: []*alipay.AccountLogItem{{AccountLogId: "log-1", TransAmount: "NaN", Direction: "收入", TransDt: "2026-11-10 08:30:00"}}}},
		{"null_entry", alipay.BillAccountLogQueryRsp{PageNo: "1", TotalSize: "1", DetailList: []*alipay.AccountLogItem{nil}}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			tt.response.Error = alipay.Error{Code: alipay.CodeSuccess}
			if tt.name != "missing_pagination" && tt.response.PageSize == "" {
				tt.response.PageSize = "1000"
			}
			p := alipayBillQueryProvider(t, func(w http.ResponseWriter, r *http.Request) { alipayBillWriteSignedResponse(t, w, tt.response) })
			start := time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC)
			if result, err := p.QueryBills(context.Background(), start, start.Add(time.Hour), 1); result != nil || infraerrors.Reason(err) != "ALIPAY_BILL_INVALID_RESPONSE" {
				t.Fatalf("malformed response produced usable statement entries: %#v, %v", result, err)
			}
		})
	}
}

func TestAlipayBillQueryParsesKeysOnlyWhenConnecting(t *testing.T) {
	_, privatePEM, _ := alipayBillSigningKeys(t)
	for _, tt := range []struct{ key, privateKey string }{{"privateKey", "private-secret"}, {"publicKey", privatePEM}} {
		t.Run(tt.key, func(t *testing.T) {
			config := alipayBillTestConfig()
			config["privateKey"], config["publicKey"] = tt.privateKey, "public-secret"
			p, err := NewAlipayBill("bill-query", config)
			if err != nil {
				t.Fatalf("configuration should be accepted before a read-only query: %v", err)
			}
			start := time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC)
			_, err = p.QueryBills(context.Background(), start, start.Add(time.Hour), 1)
			if infraerrors.Reason(err) != "ALIPAY_BILL_INVALID_KEY" || infraerrors.FromError(err).Metadata["key"] != tt.key {
				t.Fatalf("invalid key did not fail as a sanitized key error: %v", err)
			}
			if strings.Contains(err.Error(), "private-secret") || strings.Contains(err.Error(), "public-secret") {
				t.Fatalf("key error leaked configured material: %v", err)
			}
		})
	}
}

func TestAlipayBillQueryAcceptsEmptySignedPageAtQueryBounds(t *testing.T) {
	p := alipayBillQueryProvider(t, func(w http.ResponseWriter, r *http.Request) {
		alipayBillWriteSignedResponse(t, w, alipay.BillAccountLogQueryRsp{
			Error: alipay.Error{Code: alipay.CodeSuccess}, PageNo: "1000", PageSize: "1000", TotalSize: "0",
		})
	})
	start := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	result, err := p.QueryBills(context.Background(), start, start.Add(31*24*time.Hour), 1000)
	if err != nil || result == nil || result.Total != 0 || result.Page != 1000 || result.Items == nil || len(result.Items) != 0 {
		t.Fatalf("empty account-log query = %#v, %v", result, err)
	}
}

func TestAlipayBillQueryClientUsesDefaultAccountWithOnlyAppAndKeys(t *testing.T) {
	_, privatePEM, publicPEM := alipayBillSigningKeys(t)
	config := map[string]string{
		"appId": "2021001234567890", "privateKey": privatePEM, "publicKey": publicPEM,
	}
	p, err := NewAlipayBillQueryClient("bill-query-only", config)
	if err != nil {
		t.Fatalf("create query client with three credentials: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Error(err)
			return
		}
		if r.Form.Get("app_id") != config["appId"] || r.Form.Get("method") != "alipay.data.bill.accountlog.query" || r.Form.Get("app_auth_token") != "" {
			t.Error("three-credential query used an unexpected app, method, or delegated token")
		}
		var request map[string]string
		if err := json.Unmarshal([]byte(r.Form.Get("biz_content")), &request); err != nil {
			t.Error(err)
			return
		}
		if request["bill_user_id"] != "" {
			t.Error("query without a receiver should use the application's default authorized account")
		}
		alipayBillWriteSignedResponse(t, w, alipay.BillAccountLogQueryRsp{
			Error: alipay.Error{Code: alipay.CodeSuccess}, PageNo: "1", PageSize: "1000", TotalSize: "0",
		})
	}))
	t.Cleanup(server.Close)
	p.client, err = alipay.New(config["appId"], privatePEM, true, alipay.WithProductionGateway(server.URL), alipay.WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatal(err)
	}
	if err := p.client.LoadAliPayPublicKey(publicPEM); err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC)
	page, err := p.QueryBills(context.Background(), start, start.Add(time.Hour), 1)
	if err != nil || page == nil || page.Page != 1 || page.Total != 0 {
		t.Fatalf("default-account read-only query failed: %#v, %v", page, err)
	}
}
