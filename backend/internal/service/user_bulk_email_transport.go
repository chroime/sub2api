package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/smtp"
	"net/textproto"
	"sync"
	"syscall"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/balancenotice"
)

const (
	bulkSMTPMessageInterval = time.Second
	bulkSMTPRetryBackoff    = 3 * time.Second
	bulkSMTPMessagesPerConn = 20
	bulkSMTPMaxAttempts     = 2
)

// SMTP has no idempotent transaction key. After DATA starts, a missing final
// acknowledgement cannot prove whether the receiver accepted the message.
type bulkSMTPAcceptanceUnconfirmedError struct{ cause error }

func (e *bulkSMTPAcceptanceUnconfirmedError) Error() string {
	return "smtp acceptance unconfirmed: " + e.cause.Error()
}
func (e *bulkSMTPAcceptanceUnconfirmedError) Unwrap() error { return e.cause }

// bulkSMTPTransport belongs to one explicit administrator batch. The gate is
// shared by this EmailService's batches; automatic and authentication emails
// keep their independent single-message transports.
type bulkSMTPTransport struct {
	mail      *EmailService
	config    *SMTPConfig
	client    *smtp.Client
	conn      net.Conn
	completed int
	release   func()
	closeOnce sync.Once
	wait      func(context.Context, time.Duration) error
}

func newBulkSMTPTransport(ctx context.Context, mail *EmailService) (*bulkSMTPTransport, error) {
	if mail == nil {
		return nil, ErrEmailNotConfigured
	}
	mail.bulkGateOnce.Do(func() { mail.bulkGate = make(chan struct{}, 1) })
	select {
	case mail.bulkGate <- struct{}{}:
		return &bulkSMTPTransport{mail: mail, release: func() { <-mail.bulkGate }, wait: waitBulkSMTP}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func waitBulkSMTP(ctx context.Context, delay time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (t *bulkSMTPTransport) closeSession() {
	if t.client != nil {
		_ = t.client.Close()
	}
	t.client, t.conn, t.completed = nil, nil, 0
}

func (t *bulkSMTPTransport) Close() {
	if t == nil {
		return
	}
	t.closeOnce.Do(func() {
		t.closeSession()
		t.release()
	})
}

func (t *bulkSMTPTransport) Send(ctx context.Context, delivery *balancenotice.Delivery, to, subject, body string) error {
	if t.config == nil {
		config, err := t.mail.GetSMTPConfig(ctx)
		if err != nil {
			delivery.Fail(err)
			return err
		}
		t.config = config
	}
	message, err := buildSMTPMessage(t.config, to, subject, body)
	if err != nil {
		delivery.Fail(err)
		return err
	}
	for attempt := 0; attempt < bulkSMTPMaxAttempts; attempt++ {
		if err = t.wait(ctx, time.Until(t.mail.bulkLastStarted.Add(bulkSMTPMessageInterval))); err != nil {
			delivery.Fail(err)
			return err
		}
		t.mail.bulkLastStarted = time.Now()
		delivery.Attempt()
		safeToRetry := false
		err, safeToRetry = t.sendAttempt(message)
		if err == nil {
			delivery.Sent()
			return nil
		}
		t.closeSession()
		if !safeToRetry || !retryableBulkSMTPError(err) || attempt+1 >= bulkSMTPMaxAttempts {
			break
		}
		// Only an explicit transient rejection or a lost connection before DATA
		// can be retried. An absent DATA acknowledgement is never resent.
		if err = t.wait(ctx, bulkSMTPRetryBackoff); err != nil {
			break
		}
	}
	delivery.Fail(err)
	return err
}

// The boolean means no DATA body was started, so no mail could have been
// accepted in this attempt. Once Data returns a writer, failures are final.
func (t *bulkSMTPTransport) sendAttempt(message smtpMessage) (error, bool) {
	if t.completed >= bulkSMTPMessagesPerConn {
		t.closeSession()
	}
	if t.client == nil {
		client, conn, err := t.mail.connectSMTPWithConn(t.config)
		if err != nil {
			return err, true
		}
		t.client, t.conn = client, conn
		if err = client.Auth(smtp.PlainAuth("", t.config.Username, t.config.Password, t.config.Host)); err != nil {
			return fmt.Errorf("smtp auth: %w", err), true
		}
	} else {
		// Reusing a connection must not reuse the first message's absolute
		// deadline. Renew it before NOOP and every message transaction.
		if err := t.conn.SetDeadline(time.Now().Add(smtpIOTimeout)); err != nil {
			return fmt.Errorf("smtp deadline: %w", err), true
		}
		if err := t.client.Noop(); err != nil {
			return fmt.Errorf("smtp noop: %w", err), true
		}
	}
	if err := t.conn.SetDeadline(time.Now().Add(smtpIOTimeout)); err != nil {
		return fmt.Errorf("smtp deadline: %w", err), true
	}
	if err := t.client.Mail(message.envelopeFrom); err != nil {
		return fmt.Errorf("smtp mail: %w", err), true
	}
	if err := t.client.Rcpt(message.envelopeTo); err != nil {
		return fmt.Errorf("smtp rcpt: %w", err), true
	}
	writer, err := t.client.Data()
	if err != nil {
		return fmt.Errorf("smtp data: %w", err), true
	}
	if _, err = writer.Write(message.data); err != nil {
		return &bulkSMTPAcceptanceUnconfirmedError{cause: fmt.Errorf("body write: %w", err)}, false
	}
	if err = writer.Close(); err != nil {
		var response *textproto.Error
		if errors.As(err, &response) {
			return fmt.Errorf("smtp body acknowledgement: %w", err), false
		}
		return &bulkSMTPAcceptanceUnconfirmedError{cause: fmt.Errorf("body acknowledgement: %w", err)}, false
	}
	t.completed++
	return nil, false
}

func retryableBulkSMTPError(err error) bool {
	var response *textproto.Error
	if errors.As(err, &response) {
		return response.Code >= 400 && response.Code < 500
	}
	return errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, net.ErrClosed) ||
		errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.ECONNABORTED) || errors.Is(err, syscall.EPIPE)
}
