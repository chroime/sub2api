package provider

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/smartwalle/alipay/v3"
)

const (
	alipayBillTimeLayout = "2006-01-02 15:04:05"
	alipayBillPageSize   = 1000
	alipayBillMaxPage    = 1000
	alipayBillMaxRange   = 31 * 24 * time.Hour
	alipayBillTimeout    = 20 * time.Second
)

var (
	alipayBillLocation = func() *time.Location {
		location, err := time.LoadLocation("Asia/Shanghai")
		if err != nil {
			// Modern Alipay timestamps use UTC+8 even on hosts without tzdata.
			return time.FixedZone("Asia/Shanghai", 8*60*60)
		}
		return location
	}()
	alipayBillTransactionAmountPattern = regexp.MustCompile(`^-?(0|[1-9][0-9]{0,15})(\.[0-9]{1,2})?$`)
	alipayBillUpstreamCodePattern      = regexp.MustCompile(`^[0-9]{5}$`)
	alipayBillUpstreamSubCodePattern   = regexp.MustCompile(`^[A-Z][A-Z0-9_]*(\.[A-Z0-9_]+)*$`)
)

// AlipayBillEntry contains only account-log fields used for reconciliation.
// Memo text is deliberately excluded: it cannot prove an order's identity.
type AlipayBillEntry struct {
	AccountLogID    string    `json:"account_log_id"`
	AlipayOrderNo   string    `json:"alipay_order_no"`
	MerchantOrderNo string    `json:"merchant_order_no"`
	Amount          string    `json:"amount"`
	Direction       string    `json:"direction"`
	TransAt         time.Time `json:"trans_at"`
	BillSource      string    `json:"bill_source"`
}

type AlipayBillPage struct {
	Items []AlipayBillEntry `json:"items"`
	Page  int               `json:"page"`
	Total int               `json:"total"`
}

func (a *AlipayBill) getBillClient() (*alipay.Client, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.client != nil {
		return a.client, nil
	}
	client, err := alipay.New(a.config["appId"], a.config["privateKey"], true,
		alipay.WithTimeLocation(alipayBillLocation),
		alipay.WithHTTPClient(&http.Client{Timeout: alipayBillTimeout}))
	if err != nil {
		return nil, infraerrors.BadRequest("ALIPAY_BILL_INVALID_KEY", "invalid Alipay bill signing key").
			WithMetadata(map[string]string{"key": "privateKey"})
	}
	if err := client.LoadAliPayPublicKey(a.config["publicKey"]); err != nil {
		return nil, infraerrors.BadRequest("ALIPAY_BILL_INVALID_KEY", "invalid Alipay response verification key").
			WithMetadata(map[string]string{"key": "publicKey"})
	}
	a.client = client
	return client, nil
}

// QueryBills reads one signature-verified official account-log page. A configured
// receiving account is sent explicitly; otherwise a query-only client uses the
// app's default authorized account. Query success does not prove that the account
// owns the configured QR codes. An administrator must independently confirm a
// real receipt before routing payment traffic.
func (a *AlipayBill) QueryBills(ctx context.Context, start, end time.Time, page int) (*AlipayBillPage, error) {
	if start.IsZero() || end.IsZero() || !end.After(start) || end.Sub(start) > alipayBillMaxRange ||
		page < 1 || page > alipayBillMaxPage || !end.Truncate(time.Second).After(start.Truncate(time.Second)) {
		return nil, infraerrors.BadRequest("ALIPAY_BILL_INVALID_QUERY", "bill queries require a nonempty time range of at most 31 days and a page from 1 to 1000")
	}
	client, err := a.getBillClient()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, alipayBillTimeout)
	defer cancel()
	rsp, err := client.BillAccountLogQuery(ctx, alipay.BillAccountLogQuery{
		AppAuthToken: a.config["appAuthToken"], BillUserId: a.config["receiverId"],
		StartTime: start.In(alipayBillLocation).Format(alipayBillTimeLayout),
		EndTime:   end.In(alipayBillLocation).Format(alipayBillTimeLayout),
		PageNo:    strconv.Itoa(page), PageSize: strconv.Itoa(alipayBillPageSize),
	})
	if err != nil {
		var apiError *alipay.Error
		if errors.As(err, &apiError) {
			return nil, alipayBillQueryFailure(apiError)
		}
		return nil, alipayBillQueryFailure(nil)
	}
	if rsp == nil {
		return nil, alipayBillInvalidResponse()
	}
	// The SDK also treats 10003 (payment in progress) as success. A statement
	// query requires an actual successful query, not that payment-only status.
	if rsp.Code != alipay.CodeSuccess {
		return nil, alipayBillQueryFailure(&rsp.Error)
	}
	responsePage, err := strconv.Atoi(rsp.PageNo)
	if err != nil || responsePage != page {
		return nil, alipayBillInvalidResponse()
	}
	responsePageSize, err := strconv.Atoi(rsp.PageSize)
	if err != nil || responsePageSize != alipayBillPageSize {
		return nil, alipayBillInvalidResponse()
	}
	total, err := strconv.Atoi(rsp.TotalSize)
	if err != nil || total < 0 || total < len(rsp.DetailList) || len(rsp.DetailList) > alipayBillPageSize {
		return nil, alipayBillInvalidResponse()
	}
	result := &AlipayBillPage{Items: make([]AlipayBillEntry, 0, len(rsp.DetailList)), Page: responsePage, Total: total}
	for _, item := range rsp.DetailList {
		if item == nil || strings.TrimSpace(item.AccountLogId) == "" || !alipayBillTransactionAmountPattern.MatchString(item.TransAmount) {
			return nil, alipayBillInvalidResponse()
		}
		transAt, err := time.ParseInLocation(alipayBillTimeLayout, item.TransDt, alipayBillLocation)
		if err != nil {
			return nil, alipayBillInvalidResponse()
		}
		result.Items = append(result.Items, AlipayBillEntry{
			AccountLogID: strings.TrimSpace(item.AccountLogId), AlipayOrderNo: strings.TrimSpace(item.AliPayOrderNo),
			MerchantOrderNo: strings.TrimSpace(item.MerchantOrderNo), Amount: item.TransAmount,
			Direction: item.Direction, TransAt: transAt, BillSource: item.BillSource,
		})
	}
	return result, nil
}

func alipayBillQueryFailure(upstream *alipay.Error) error {
	metadata := make(map[string]string)
	if upstream != nil {
		if alipayBillUpstreamCodePattern.MatchString(string(upstream.Code)) {
			metadata["upstream_code"] = string(upstream.Code)
		}
		if len(upstream.SubCode) <= 64 && alipayBillUpstreamSubCodePattern.MatchString(upstream.SubCode) {
			metadata["sub_code"] = upstream.SubCode
		}
	}
	// Neither the raw response nor the original SDK error is attached as a cause:
	// HTTP/SDK errors can contain signed request URLs, tokens, or account data.
	return infraerrors.New(http.StatusBadGateway, "ALIPAY_BILL_QUERY_FAILED", "Alipay account-log query failed").WithMetadata(metadata)
}

func alipayBillInvalidResponse() error {
	return infraerrors.New(http.StatusBadGateway, "ALIPAY_BILL_INVALID_RESPONSE", "Alipay returned an invalid account-log response")
}
