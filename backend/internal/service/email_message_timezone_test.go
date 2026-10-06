package service

import (
	"bytes"
	"net/mail"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestBuildSMTPMessageDateUsesBeijingTimeWithoutShiftingInstant(t *testing.T) {
	start := time.Now().Truncate(time.Second)
	message, err := buildSMTPMessage(&SMTPConfig{Host: "smtp.example.test", From: "sender@example.test"}, "admin@example.test", "时区测试", "正文")
	end := time.Now()
	require.NoError(t, err)
	parsed, err := mail.ReadMessage(bytes.NewReader(message.data))
	require.NoError(t, err)
	sent, err := mail.ParseDate(parsed.Header.Get("Date"))
	require.NoError(t, err)
	_, offset := sent.Zone()
	require.Equal(t, 8*60*60, offset)
	require.False(t, sent.Before(start), "timezone conversion must not shift the send instant")
	require.False(t, sent.After(end), "timezone conversion must not shift the send instant")
}
