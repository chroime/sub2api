package service

import (
	"context"
	"crypto/hmac"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"entgo.io/ent/dialect"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/paymentproviderinstance"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/Wei-Shaw/sub2api/internal/payment/epay"
	"github.com/Wei-Shaw/sub2api/internal/payment/provider"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/google/uuid"
)

type BuiltinEasyPayOrder struct {
	ID                   int64      `json:"id"`
	ProviderInstanceID   int64      `json:"provider_instance_id"`
	OutTradeNo           string     `json:"out_trade_no"`
	TradeNo              string     `json:"trade_no"`
	AmountMinor          int64      `json:"-"`
	Amount               string     `json:"amount"`
	ReceiverID           string     `json:"-"`
	AppID                string     `json:"-"`
	QRRuleID             string     `json:"-"`
	QRURL                string     `json:"-"`
	Name                 string     `json:"name"`
	Param                string     `json:"param,omitempty"`
	NotifyURL            string     `json:"-"`
	ReturnURL            string     `json:"-"`
	CallbackKeyEncrypted string     `json:"-"`
	Status               string     `json:"status"`
	AlipayOrderNo        string     `json:"alipay_order_no,omitempty"`
	CreatedAt            time.Time  `json:"created_at"`
	ExpiresAt            time.Time  `json:"expires_at"`
	PaidAt               *time.Time `json:"paid_at,omitempty"`
	UpdatedAt            time.Time  `json:"updated_at"`
	NotifyStatus         string     `json:"notify_status"`
	NotifyAttempts       int        `json:"notify_attempts"`
	NotifyNextAt         *time.Time `json:"notify_next_at,omitempty"`
	NotifyLeaseUntil     *time.Time `json:"-"`
	NotifyLeaseOwner     string     `json:"-"`
	NotifyLastError      string     `json:"notify_last_error,omitempty"`
	NotifyDeliveredAt    *time.Time `json:"notify_delivered_at,omitempty"`
}

type BuiltinEasyPayOrderPage struct {
	Items    []BuiltinEasyPayOrder `json:"items"`
	Total    int                   `json:"total"`
	Page     int                   `json:"page"`
	PageSize int                   `json:"page_size"`
}

type BuiltinEasyPayCreateResponse struct {
	Code      int       `json:"code"`
	Msg       string    `json:"msg"`
	TradeNo   string    `json:"trade_no"`
	PayURL    string    `json:"payurl"`
	QRCode    string    `json:"qrcode"`
	ExpiresAt time.Time `json:"expires_at"`
}

type BuiltinEasyPayQueryResponse struct {
	Code        int    `json:"code"`
	Msg         string `json:"msg"`
	PID         string `json:"pid"`
	TradeNo     string `json:"trade_no"`
	OutTradeNo  string `json:"out_trade_no"`
	Type        string `json:"type"`
	Name        string `json:"name"`
	Money       string `json:"money"`
	Status      int    `json:"status"`
	TradeStatus string `json:"trade_status,omitempty"`
}

const builtinEasyPayOrderColumns = `id,provider_instance_id,out_trade_no,trade_no,amount_minor,receiver_id,app_id,qr_rule_id,qr_url,name,param,notify_url,return_url,callback_key_encrypted,status,alipay_order_no,created_at,expires_at,paid_at,updated_at,notify_status,notify_attempts,notify_next_at,notify_lease_until,notify_lease_owner,notify_last_error,notify_delivered_at`

func scanBuiltinEasyPayOrder(rows interface{ Scan(...any) error }) (BuiltinEasyPayOrder, error) {
	var o BuiltinEasyPayOrder
	var paid, next, lease, delivered sql.NullTime
	err := rows.Scan(&o.ID, &o.ProviderInstanceID, &o.OutTradeNo, &o.TradeNo, &o.AmountMinor, &o.ReceiverID, &o.AppID, &o.QRRuleID, &o.QRURL,
		&o.Name, &o.Param, &o.NotifyURL, &o.ReturnURL, &o.CallbackKeyEncrypted, &o.Status, &o.AlipayOrderNo, &o.CreatedAt, &o.ExpiresAt, &paid, &o.UpdatedAt,
		&o.NotifyStatus, &o.NotifyAttempts, &next, &lease, &o.NotifyLeaseOwner, &o.NotifyLastError, &delivered)
	o.Amount = fmt.Sprintf("%d.%02d", o.AmountMinor/100, o.AmountMinor%100)
	if paid.Valid {
		o.PaidAt = &paid.Time
	}
	if next.Valid {
		o.NotifyNextAt = &next.Time
	}
	if lease.Valid {
		o.NotifyLeaseUntil = &lease.Time
	}
	if delivered.Valid {
		o.NotifyDeliveredAt = &delivered.Time
	}
	return o, err
}

var builtinEasyPayOrderRef = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

var builtinEasyPayCreateFields = map[string]bool{
	"pid": true, "type": true, "out_trade_no": true, "notify_url": true, "return_url": true, "name": true, "money": true, "clientip": true,
	"cid": true, "device": true, "param": true, "sign": true, "sign_type": true,
}

func builtinEasyPayAuthError() error {
	return infraerrors.Unauthorized("BUILTIN_EPAY_AUTH_FAILED", "invalid merchant credentials or signature")
}

func validateBuiltinEasyPayFields(params map[string]string, allowed map[string]bool) error {
	for k, value := range params {
		if !allowed[k] || len(value) > 4096 || !utf8.ValidString(value) {
			return infraerrors.BadRequest("BUILTIN_EPAY_INVALID_REQUEST", "unsupported or oversized request parameter")
		}
		for _, r := range value {
			if unicode.IsControl(r) {
				return infraerrors.BadRequest("BUILTIN_EPAY_INVALID_REQUEST", "request parameters must not contain control characters")
			}
		}
	}
	return nil
}

func builtinEasyPayProviderID(raw string) (int64, error) {
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 || strconv.FormatInt(id, 10) != raw {
		return 0, builtinEasyPayAuthError()
	}
	return id, nil
}

func findBuiltinEasyPayOrder(ctx context.Context, client *dbent.Client, providerID int64, outTradeNo, tradeNo string) (*BuiltinEasyPayOrder, error) {
	rows, err := client.QueryContext(ctx, `SELECT `+builtinEasyPayOrderColumns+` FROM payment_epay_orders WHERE provider_instance_id=$1 AND (($2<>'' AND out_trade_no=$2) OR ($3<>'' AND trade_no=$3))`, providerID, outTradeNo, tradeNo)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		if rows.Err() != nil {
			return nil, rows.Err()
		}
		return nil, sql.ErrNoRows
	}
	o, err := scanBuiltinEasyPayOrder(rows)
	if err != nil {
		return nil, err
	}
	if rows.Next() || (outTradeNo != "" && o.OutTradeNo != outTradeNo) || (tradeNo != "" && o.TradeNo != tradeNo) {
		return nil, infraerrors.BadRequest("BUILTIN_EPAY_INVALID_REQUEST", "order identifiers do not identify the same order")
	}
	return &o, rows.Err()
}

func (s *PaymentService) builtinEasyPayOrderKey(o *BuiltinEasyPayOrder) (string, error) {
	if s.configService == nil {
		return "", builtinEasyPayAuthError()
	}
	key, err := payment.Decrypt(o.CallbackKeyEncrypted, s.configService.encryptionKey)
	if err != nil || !validBuiltinEasyPayKey(key) {
		return "", infraerrors.ServiceUnavailable("BUILTIN_EPAY_KEY_UNAVAILABLE", "stored merchant credentials are unavailable")
	}
	return key, nil
}

// CreateBuiltinEasyPayOrder creates an external merchant order. It never calls
// CreateOrder or links the payment to a local user's balance or subscription.
func (s *PaymentService) CreateBuiltinEasyPayOrder(ctx context.Context, params map[string]string) (*BuiltinEasyPayCreateResponse, error) {
	if err := validateBuiltinEasyPayFields(params, builtinEasyPayCreateFields); err != nil {
		return nil, err
	}
	if params["type"] != payment.TypeAlipay {
		return nil, infraerrors.BadRequest("BUILTIN_EPAY_UNSUPPORTED_TYPE", "only alipay QR payments are supported")
	}
	if params["sign_type"] != "MD5" || !builtinEasyPayOrderRef.MatchString(params["out_trade_no"]) || strings.TrimSpace(params["name"]) == "" || utf8.RuneCountInString(params["name"]) > 256 || utf8.RuneCountInString(params["param"]) > 512 || strings.ContainsAny(params["param"], "&=") {
		return nil, infraerrors.BadRequest("BUILTIN_EPAY_INVALID_REQUEST", "invalid order identity, name, parameter, or signature type")
	}
	if params["cid"] != "" {
		return nil, infraerrors.BadRequest("BUILTIN_EPAY_UNSUPPORTED_CHANNEL", "channel overrides are not supported")
	}
	providerID, err := builtinEasyPayProviderID(params["pid"])
	if err != nil {
		return nil, err
	}
	cents, err := provider.ParseAlipayBillAmountCents(params["money"])
	if err != nil {
		return nil, err
	}
	if s.configService == nil || s.entClient == nil {
		return nil, builtinEasyPayAuthError()
	}
	// Read global settings outside the provider transaction, which serializes
	// against credential/identity changes and new-order disablement.
	globalEnabled := false
	orderTimeoutMin := defaultOrderTimeoutMin
	if s.configService.settingRepo != nil {
		globalConfig, err := s.configService.GetPaymentConfig(ctx)
		if err != nil {
			return nil, err
		}
		globalEnabled = globalConfig.Enabled
		if globalConfig.OrderTimeoutMin > 0 {
			orderTimeoutMin = globalConfig.OrderTimeoutMin
		}
	}
	tx, err := s.entClient.Tx(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	query := tx.PaymentProviderInstance.Query().Where(paymentproviderinstance.IDEQ(providerID), paymentproviderinstance.ProviderKeyEQ(payment.TypeAlipayBill))
	if s.entClient.Driver().Dialect() == dialect.Postgres {
		query = query.ForUpdate()
	}
	inst, err := query.Only(ctx)
	if err != nil {
		if dbent.IsNotFound(err) {
			return nil, builtinEasyPayAuthError()
		}
		return nil, err
	}
	cfg, err := s.configService.decryptProviderConfig(inst.ProviderKey, inst.Config)
	if err != nil {
		return nil, err
	}
	existing, err := findBuiltinEasyPayOrder(ctx, tx.Client(), providerID, params["out_trade_no"], "")
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if existing != nil {
		key, e := s.builtinEasyPayOrderKey(existing)
		if e != nil {
			return nil, e
		}
		if !epay.Verify(params, key, params["sign"]) && !(validBuiltinEasyPayKey(cfg["epayKey"]) && epay.Verify(params, cfg["epayKey"], params["sign"])) {
			return nil, builtinEasyPayAuthError()
		}
		// Replaying the original immutable request must not create another order,
		// even after changing the source, disabling new orders, or rotating rules.
		notifyURL, _, e := parseBuiltinEasyPayURL(params["notify_url"])
		if e != nil {
			return nil, e
		}
		returnURL := ""
		if params["return_url"] != "" {
			u, _, e := parseBuiltinEasyPayURL(params["return_url"])
			if e != nil {
				return nil, e
			}
			returnURL = u.String()
		}
		if existing.AmountMinor != cents || existing.Name != params["name"] || existing.Param != params["param"] || existing.NotifyURL != notifyURL.String() || existing.ReturnURL != returnURL {
			return nil, infraerrors.Conflict("BUILTIN_EPAY_ORDER_CONFLICT", "merchant order number already exists with different immutable payment data")
		}
		if existing.Status == "EXPIRED" || (existing.Status == "PENDING" && !existing.ExpiresAt.After(time.Now())) {
			return nil, infraerrors.Conflict("BUILTIN_EPAY_ORDER_EXPIRED", "this gateway order has expired; use a new merchant order number")
		}
		if existing.Status != "PENDING" {
			return nil, infraerrors.Conflict("BUILTIN_EPAY_ORDER_PAID", "this gateway order is already paid; query its status")
		}
		return builtinEasyPayCreateResponse(existing), nil
	}
	if !validBuiltinEasyPayKey(cfg["epayKey"]) || !epay.Verify(params, cfg["epayKey"], params["sign"]) {
		return nil, builtinEasyPayAuthError()
	}
	if !globalEnabled || !inst.Enabled || cfg["epayEnabled"] != "true" {
		return nil, infraerrors.Forbidden("BUILTIN_EPAY_DISABLED", "new gateway orders are disabled")
	}
	if err := ValidateBuiltinEasyPayConfig(cfg); err != nil {
		return nil, err
	}
	origins, err := parseBuiltinEasyPayOrigins(cfg["epayNotifyOrigins"])
	if err != nil {
		return nil, err
	}
	notifyURL, err := validateBuiltinEasyPayURL(params["notify_url"], origins, true)
	if err != nil {
		return nil, err
	}
	notifyParsed, _, err := parseBuiltinEasyPayURL(notifyURL)
	if err != nil {
		return nil, err
	}
	if notifyParsed.RawQuery != "" || notifyParsed.ForceQuery {
		return nil, infraerrors.BadRequest("BUILTIN_EPAY_INVALID_URL", "notify_url must not contain query parameters; use the signed param field for merchant metadata")
	}
	returnURL, err := validateBuiltinEasyPayURL(params["return_url"], origins, false)
	if err != nil {
		return nil, err
	}
	reader, err := provider.NewAlipayBill(params["pid"], cfg)
	if err != nil {
		return nil, err
	}
	created, err := reader.CreatePayment(ctx, payment.CreatePaymentRequest{Amount: params["money"], PaymentType: payment.TypeAlipay})
	if err != nil {
		return nil, err
	}
	secret, err := payment.Encrypt(cfg["epayKey"], s.configService.encryptionKey)
	if err != nil {
		return nil, infraerrors.ServiceUnavailable("BUILTIN_EPAY_KEY_UNAVAILABLE", "merchant credentials cannot be stored securely")
	}
	receiver := strings.TrimSpace(cfg["receiverId"])
	if err := s.lockBillAmountReservation(ctx, tx, receiver, cents, time.Now().UTC()); err != nil {
		return nil, err
	}
	// Align the order window with official bill timestamp precision.
	now := time.Now().UTC().Truncate(time.Second)
	expiresAt := now.Add(time.Duration(orderTimeoutMin) * time.Minute)
	tradeNo := "epay_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	rows, err := tx.Client().QueryContext(ctx, `INSERT INTO payment_epay_orders(provider_instance_id,out_trade_no,trade_no,amount_minor,receiver_id,app_id,qr_rule_id,qr_url,name,param,notify_url,return_url,callback_key_encrypted,status,created_at,expires_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,'PENDING',$14,$15,$14) RETURNING `+builtinEasyPayOrderColumns,
		providerID, params["out_trade_no"], tradeNo, cents, receiver, strings.TrimSpace(cfg["appId"]), created.QRRuleID, created.QRCode, params["name"], params["param"], notifyURL, returnURL, secret, now, expiresAt)
	if err != nil {
		return nil, err
	}
	if !rows.Next() {
		e := rows.Err()
		rows.Close()
		if e != nil {
			return nil, e
		}
		return nil, fmt.Errorf("gateway order insert returned no row")
	}
	o, err := scanBuiltinEasyPayOrder(rows)
	rows.Close()
	if err != nil {
		return nil, err
	}
	if err := s.insertBillAmountReservation(ctx, tx, providerID, receiver, cents, 0, o.ID, now, expiresAt); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return builtinEasyPayCreateResponse(&o), nil
}

func builtinEasyPayCreateResponse(o *BuiltinEasyPayOrder) *BuiltinEasyPayCreateResponse {
	return &BuiltinEasyPayCreateResponse{Code: 1, Msg: "success", TradeNo: o.TradeNo, PayURL: o.QRURL, QRCode: o.QRURL, ExpiresAt: o.ExpiresAt}
}

func (s *PaymentService) QueryBuiltinEasyPayOrder(ctx context.Context, params map[string]string) (*BuiltinEasyPayQueryResponse, error) {
	if err := validateBuiltinEasyPayFields(params, map[string]bool{"act": true, "pid": true, "key": true, "out_trade_no": true, "trade_no": true}); err != nil {
		return nil, err
	}
	if params["act"] != "order" {
		return nil, infraerrors.BadRequest("BUILTIN_EPAY_UNSUPPORTED_ACTION", "only order queries are supported; refunds are manual")
	}
	pid, err := builtinEasyPayProviderID(params["pid"])
	if err != nil {
		return nil, err
	}
	if (params["out_trade_no"] == "" && params["trade_no"] == "") || (params["out_trade_no"] != "" && !builtinEasyPayOrderRef.MatchString(params["out_trade_no"])) || (params["trade_no"] != "" && !builtinEasyPayOrderRef.MatchString(params["trade_no"])) {
		return nil, builtinEasyPayAuthError()
	}
	o, err := findBuiltinEasyPayOrder(ctx, s.entClient, pid, params["out_trade_no"], params["trade_no"])
	if errors.Is(err, sql.ErrNoRows) {
		return nil, builtinEasyPayAuthError()
	}
	if err != nil {
		return nil, err
	}
	key, err := s.builtinEasyPayOrderKey(o)
	if err != nil {
		return nil, err
	}
	valid := hmac.Equal([]byte(key), []byte(params["key"]))
	if !valid {
		inst, e := s.entClient.PaymentProviderInstance.Get(ctx, pid)
		if e == nil && inst.ProviderKey == payment.TypeAlipayBill {
			cfg, e := s.configService.decryptProviderConfig(inst.ProviderKey, inst.Config)
			valid = e == nil && validBuiltinEasyPayKey(cfg["epayKey"]) && hmac.Equal([]byte(cfg["epayKey"]), []byte(params["key"]))
		}
	}
	if !valid {
		return nil, builtinEasyPayAuthError()
	}
	result := &BuiltinEasyPayQueryResponse{Code: 1, Msg: "success", PID: params["pid"], TradeNo: o.TradeNo, OutTradeNo: o.OutTradeNo, Type: payment.TypeAlipay, Name: o.Name, Money: o.Amount}
	if o.Status == "PAID" && o.PaidAt != nil && o.AlipayOrderNo != "" {
		result.Status = 1
		result.TradeStatus = "TRADE_SUCCESS"
	}
	return result, nil
}

func (s *PaymentService) ListBuiltinEasyPayOrders(ctx context.Context, providerID int64, page, size int, status, keyword string) (*BuiltinEasyPayOrderPage, error) {
	inst, err := s.entClient.PaymentProviderInstance.Get(ctx, providerID)
	if err != nil {
		return nil, err
	}
	if inst.ProviderKey != payment.TypeAlipayBill {
		return nil, infraerrors.BadRequest("ALIPAY_BILL_PROVIDER_REQUIRED", "select a built-in fixed-QR provider")
	}
	if status != "" && status != "PENDING" && status != "PAID" && status != "EXPIRED" {
		return nil, infraerrors.BadRequest("VALIDATION_ERROR", "invalid gateway order status")
	}
	if len(keyword) > 128 {
		return nil, infraerrors.BadRequest("VALIDATION_ERROR", "order search is too long")
	}
	size, page = applyPagination(size, page)
	if page > 100000 {
		return nil, infraerrors.BadRequest("VALIDATION_ERROR", "page is too large")
	}
	result := &BuiltinEasyPayOrderPage{Items: []BuiltinEasyPayOrder{}, Page: page, PageSize: size}
	predicate := ` FROM payment_epay_orders WHERE provider_instance_id=$1 AND ($2='' OR status=$2) AND ($3='' OR out_trade_no=$3 OR trade_no=$3)`
	rows, err := s.entClient.QueryContext(ctx, `SELECT COUNT(*)`+predicate, providerID, status, strings.TrimSpace(keyword))
	if err != nil {
		return nil, err
	}
	if rows.Next() {
		err = rows.Scan(&result.Total)
	}
	rowErr := rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if rowErr != nil {
		return nil, rowErr
	}
	rows, err = s.entClient.QueryContext(ctx, `SELECT `+builtinEasyPayOrderColumns+predicate+` ORDER BY created_at DESC,id DESC LIMIT $4 OFFSET $5`, providerID, status, strings.TrimSpace(keyword), size, (page-1)*size)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		o, err := scanBuiltinEasyPayOrder(rows)
		if err != nil {
			return nil, err
		}
		result.Items = append(result.Items, o)
	}
	return result, rows.Err()
}
