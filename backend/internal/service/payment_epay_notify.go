package service

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/payment/epay"
	"github.com/google/uuid"
)

const builtinEasyPayNotifyTimeout = 8 * time.Second

// builtinEasyPayNotifyClient validates and pins each dialed address, rather than
// resolving once during configuration validation and trusting a later DNS lookup.
// Proxy environment settings and redirects cannot bypass this destination check.
func builtinEasyPayNotifyClient(raw string) (*http.Client, error) {
	u, _, err := parseBuiltinEasyPayURL(raw)
	if err != nil {
		return nil, err
	}
	expectedHost := strings.ToLower(u.Hostname())
	expectedPort := u.Port()
	if expectedPort == "" {
		if u.Scheme == "https" {
			expectedPort = "443"
		} else {
			expectedPort = "80"
		}
	}
	allowLoopback := builtinEasyPayLoopbackHost(expectedHost)
	dialer := &net.Dialer{Timeout: builtinEasyPayNotifyTimeout}
	transport := &http.Transport{
		Proxy: nil, DisableKeepAlives: true, TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 5 * time.Second,
		TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
	}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, e := net.SplitHostPort(address)
		if e != nil || !strings.EqualFold(host, expectedHost) || port != expectedPort {
			return nil, fmt.Errorf("callback destination changed")
		}
		var ips []net.IPAddr
		if ip := net.ParseIP(host); ip != nil {
			ips = []net.IPAddr{{IP: ip}}
		} else {
			ips, e = net.DefaultResolver.LookupIPAddr(ctx, host)
			if e != nil {
				return nil, fmt.Errorf("callback DNS lookup failed")
			}
		}
		if len(ips) == 0 {
			return nil, fmt.Errorf("callback DNS lookup returned no address")
		}
		// Reject a mixed public/private answer in full. Loopback is permitted
		// only for an explicitly saved localhost/loopback development origin.
		for _, ip := range ips {
			if ip.Zone != "" || !(builtinEasyPayPublicIP(ip.IP) || (allowLoopback && ip.IP.IsLoopback())) {
				return nil, fmt.Errorf("callback address is not permitted")
			}
		}
		for _, ip := range ips {
			conn, e := dialer.DialContext(ctx, network, net.JoinHostPort(ip.IP.String(), port))
			if e == nil {
				return conn, nil
			}
		}
		return nil, fmt.Errorf("callback connection failed")
	}
	return &http.Client{Timeout: builtinEasyPayNotifyTimeout, Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, nil
}

func builtinEasyPayNotificationParams(o *BuiltinEasyPayOrder, key string) map[string]string {
	params := map[string]string{"pid": strconv.FormatInt(o.ProviderInstanceID, 10), "trade_no": o.TradeNo, "out_trade_no": o.OutTradeNo,
		"type": "alipay", "name": o.Name, "money": o.Amount, "trade_status": "TRADE_SUCCESS"}
	if o.Param != "" {
		params["param"] = o.Param
	}
	params["sign"] = epay.Sign(params, key)
	params["sign_type"] = "MD5"
	return params
}

func (s *PaymentService) sendBuiltinEasyPayNotification(ctx context.Context, o *BuiltinEasyPayOrder) string {
	key, err := s.builtinEasyPayOrderKey(o)
	if err != nil {
		return "key_unavailable"
	}
	client, err := builtinEasyPayNotifyClient(o.NotifyURL)
	if err != nil {
		return "invalid_callback_url"
	}
	defer client.CloseIdleConnections()
	u, err := url.Parse(o.NotifyURL)
	if err != nil {
		return "invalid_callback_url"
	}
	values := u.Query()
	for k, v := range builtinEasyPayNotificationParams(o, key) {
		values.Set(k, v)
	}
	u.RawQuery = values.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "invalid_callback_url"
	}
	req.Header.Set("User-Agent", "Sub2API-EasyPay-Notify/1")
	resp, err := client.Do(req)
	if err != nil {
		return "callback_transport_failed"
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "callback_http_status"
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 65))
	if err != nil {
		return "callback_read_failed"
	}
	if len(body) > 64 || strings.TrimSpace(string(body)) != "success" {
		return "callback_not_acknowledged"
	}
	return ""
}

func builtinEasyPayRetryDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if attempt > 11 {
		attempt = 11
	}
	delay := 30 * time.Second * time.Duration(1<<uint(attempt-1))
	if delay > 6*time.Hour {
		return 6 * time.Hour
	}
	return delay
}

// RetryBuiltinEasyPayNotifications processes a bounded batch of durable paid
// orders. Leases prevent two workers sending the same attempt concurrently;
// callers must still treat notifications as at-least-once delivery.
func (s *PaymentService) RetryBuiltinEasyPayNotifications(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	now := time.Now().UTC()
	if _, err := s.entClient.ExecContext(ctx, `UPDATE payment_epay_orders SET status='EXPIRED',updated_at=$1 WHERE status='PENDING' AND expires_at<=$1`, now); err != nil {
		return err
	}
	rows, err := s.entClient.QueryContext(ctx, `SELECT id FROM payment_epay_orders WHERE status='PAID' AND notify_status='pending' AND (notify_next_at IS NULL OR notify_next_at<=$1) AND (notify_lease_until IS NULL OR notify_lease_until<=$1) ORDER BY notify_next_at,id LIMIT 20`, now)
	if err != nil {
		return err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return err
		}
		now = time.Now().UTC()
		owner := uuid.NewString()
		rows, err := s.entClient.QueryContext(ctx, `UPDATE payment_epay_orders SET notify_lease_owner=$1,notify_lease_until=$2,notify_attempts=notify_attempts+1,updated_at=$3 WHERE id=$4 AND status='PAID' AND notify_status='pending' AND (notify_next_at IS NULL OR notify_next_at<=$3) AND (notify_lease_until IS NULL OR notify_lease_until<=$3) RETURNING `+builtinEasyPayOrderColumns, owner, now.Add(30*time.Second), now, id)
		if err != nil {
			return err
		}
		if !rows.Next() {
			err = rows.Err()
			rows.Close()
			if err != nil {
				return err
			}
			continue
		}
		order, err := scanBuiltinEasyPayOrder(rows)
		rows.Close()
		if err != nil {
			return err
		}
		errorCode := s.sendBuiltinEasyPayNotification(ctx, &order)
		// The request context may expire after sending. Save its outcome using
		// an independent short context; a crash remains retryable via the lease.
		saveCtx, saveCancel := context.WithTimeout(context.Background(), 3*time.Second)
		now = time.Now().UTC()
		if errorCode == "" {
			_, err = s.entClient.ExecContext(saveCtx, `UPDATE payment_epay_orders SET notify_status='delivered',notify_delivered_at=$1,notify_next_at=NULL,notify_lease_until=NULL,notify_lease_owner='',notify_last_error='',updated_at=$1 WHERE id=$2 AND notify_lease_owner=$3 AND notify_status='pending'`, now, id, owner)
		} else {
			_, err = s.entClient.ExecContext(saveCtx, `UPDATE payment_epay_orders SET notify_next_at=$1,notify_lease_until=NULL,notify_lease_owner='',notify_last_error=$2,updated_at=$3 WHERE id=$4 AND notify_lease_owner=$5 AND notify_status='pending'`, now.Add(builtinEasyPayRetryDelay(order.NotifyAttempts)), errorCode, now, id, owner)
		}
		saveCancel()
		if err != nil {
			return err
		}
	}
	return nil
}
