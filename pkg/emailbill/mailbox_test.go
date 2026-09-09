package emailbill

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFetchRecentMessageBatchesSkipsEmptyBatchesAndStopsAtLimit(t *testing.T) {
	uids := make([]uint32, 120)
	for i := range uids {
		uids[i] = uint32(i + 1)
	}
	var batches [][]uint32
	var limits []uint32
	messages, err := fetchRecentMessageBatches(context.Background(), uids, 3, func(batch []uint32, limit uint32) ([]Message, error) {
		batches = append(batches, batch)
		limits = append(limits, limit)
		switch len(batches) {
		case 1:
			return nil, nil // Already processed or unmatched mail must not consume the limit.
		case 2:
			return []Message{{MessageID: "newest"}, {MessageID: "next"}}, nil
		default:
			return []Message{{MessageID: "oldest"}}, nil
		}
	})
	require.NoError(t, err)
	require.Len(t, messages, 3)
	require.Len(t, batches, 3)
	assert.Equal(t, []uint32{3, 3, 1}, limits)
	assert.Equal(t, uint32(120), batches[0][len(batches[0])-1])
	assert.Greater(t, batches[0][0], batches[1][len(batches[1])-1])
	for _, batch := range batches {
		assert.LessOrEqual(t, len(batch), imapFetchBatchSize)
	}
}

func TestFetchRecentMessageBatchesPreservesFailuresAndCancellation(t *testing.T) {
	want := errors.New("IMAP connection closed")
	_, err := fetchRecentMessageBatches(context.Background(), []uint32{1}, 1, func([]uint32, uint32) ([]Message, error) {
		return nil, want
	})
	require.ErrorIs(t, err, want)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = fetchRecentMessageBatches(ctx, []uint32{1}, 1, func([]uint32, uint32) ([]Message, error) {
		t.Fatal("canceled request must not fetch mail")
		return nil, nil
	})
	require.ErrorIs(t, err, context.Canceled)
}

func TestDecodeMessageExtractsMultipartTextAndAuthenticatesBank(t *testing.T) {
	raw := strings.Join([]string{
		"From: =?UTF-8?B?5oub5ZWG6ZO26KGM?= <95555@message.cmbchina.com>",
		"Subject: =?UTF-8?B?5oub5ZWG6ZO26KGM6YCa55+l?=",
		"Message-ID: <bill-1@example.com>",
		"Date: Mon, 7 Sep 2026 09:00:00 +0800",
		"Authentication-Results: mx.qq.com; dkim=pass header.d=message.cmbchina.com; spf=pass smtp.mailfrom=95555@message.cmbchina.com",
		"Content-Type: multipart/alternative; boundary=mail-boundary",
		"",
		"--mail-boundary",
		"Content-Type: text/plain; charset=utf-8",
		"Content-Transfer-Encoding: quoted-printable",
		"",
		"=E6=82=A8=E7=9A=84=E8=B4=A6=E6=88=B7=E4=BA=8E09=E6=9C=8807=E6=97=A508:30=E5=9C=A8=E6=97=A9=E9=A4=90=E5=BA=97=E6=94=AF=E4=BB=9812.34=E5=85=83=E3=80=82",
		"--mail-boundary--",
		"",
	}, "\r\n")

	message, err := DecodeMessage(strings.NewReader(raw), MessageSecurity{
		RequireAuthenticationResults: true,
		TrustedAuthservDomains:       []string{"qq.com"},
	}, time.Time{})

	require.NoError(t, err)
	assert.Equal(t, "95555@message.cmbchina.com", message.Sender)
	assert.Equal(t, "招商银行通知", message.Subject)
	assert.Contains(t, message.Text, "早餐店支付12.34元")
	assert.True(t, message.Authenticated)
	assert.Equal(t, "<bill-1@example.com>", message.Fingerprint)
}

func TestDecodeMessageRejectsUntrustedAuthenticationResults(t *testing.T) {
	raw := strings.Join([]string{
		"From: 95555@message.cmbchina.com",
		"Subject: 招商银行通知",
		"Authentication-Results: mx.qq.com.evil.example; dkim=pass header.d=message.cmbchina.com",
		"Content-Type: text/plain; charset=utf-8",
		"",
		"test",
		"",
	}, "\r\n")

	message, err := DecodeMessage(strings.NewReader(raw), MessageSecurity{
		RequireAuthenticationResults: true,
		TrustedAuthservDomains:       []string{"qq.com"},
	}, time.Now())

	require.NoError(t, err)
	assert.False(t, message.Authenticated)
}

func TestDecodeMessagePreservesSafeHeadersForUserParsers(t *testing.T) {
	raw := strings.Join([]string{
		"From: Bank <bank@example.com>",
		"To: alice@example.com",
		"Subject: Monthly card bill",
		"Message-ID: <bill-42@example.com>",
		"Date: Tue, 8 Sep 2026 10:00:00 +0800",
		"Authentication-Results: mx.example.com; dkim=pass header.d=example.com",
		"Content-Type: text/plain; charset=utf-8",
		"",
		"Coffee 12.00 CNY",
	}, "\r\n")

	message, err := DecodeMessage(strings.NewReader(raw), MessageSecurity{}, time.Time{})
	require.NoError(t, err)

	assert.Equal(t, "<bill-42@example.com>", message.MessageID)
	assert.Equal(t, "alice@example.com", message.Headers["to"])
	assert.Equal(t, "Monthly card bill", message.Headers["subject"])
	assert.NotContains(t, message.Headers, "authorization")
}

func TestNewestMessageDatesAppliesBatchLimitAfterFiltering(t *testing.T) {
	dates := map[uint32]time.Time{
		1: time.Unix(1, 0), 2: time.Unix(2, 0), 3: time.Unix(3, 0), 4: time.Unix(4, 0),
	}

	limited := newestMessageDates(dates, 2)

	assert.Len(t, limited, 2)
	assert.Contains(t, limited, uint32(3))
	assert.Contains(t, limited, uint32(4))
}

func TestIMAPMailboxWithoutBuiltInParsersAcceptsMailForUserRules(t *testing.T) {
	mailbox := NewIMAPMailbox(MailboxConfig{}, nil)

	assert.True(t, mailbox.supported(Message{Sender: "any-bank@example.com", Subject: "any subject"}))
}

func TestDecodeMessageAcceptsDKIMIdentityDomain(t *testing.T) {
	raw := strings.Join([]string{
		"From: 95555@message.cmbchina.com",
		"Subject: 招商银行通知",
		"Authentication-Results: mx.qq.com; dkim=pass header.i=@message.cmbchina.com",
		"Content-Type: text/plain; charset=utf-8",
		"",
		"test",
		"",
	}, "\r\n")

	message, err := DecodeMessage(strings.NewReader(raw), MessageSecurity{
		RequireAuthenticationResults: true,
		TrustedAuthservDomains:       []string{"qq.com"},
	}, time.Now())

	require.NoError(t, err)
	assert.True(t, message.Authenticated)
}

func TestDecodeMessageRejectsOversizedBody(t *testing.T) {
	raw := "From: 95555@message.cmbchina.com\r\nSubject: 招商银行通知\r\nContent-Type: text/plain\r\n\r\n" + strings.Repeat("x", 128)

	_, err := DecodeMessageWithLimit(strings.NewReader(raw), MessageSecurity{}, time.Now(), 64)

	require.ErrorContains(t, err, "size limit")
}

func TestDecodeMessageHeadersAcceptsMultipartWithoutBody(t *testing.T) {
	raw := strings.Join([]string{
		"From: Bank <95555@message.cmbchina.com>",
		"Subject: =?UTF-8?B?5oub5ZWG6ZO26KGM6YCa55+l?=",
		"Message-ID: <multipart-bill@example.com>",
		"Date: Mon, 7 Sep 2026 09:00:00 +0800",
		"Authentication-Results: mx.qq.com; dkim=pass header.d=message.cmbchina.com",
		"Content-Type: multipart/alternative; boundary=mail-boundary",
		"", "",
	}, "\r\n")
	security := MessageSecurity{RequireAuthenticationResults: true, TrustedAuthservDomains: []string{"qq.com"}}

	message, err := DecodeMessageHeadersWithLimit(strings.NewReader(raw), security, time.Time{}, DefaultMaxMessageBytes)

	require.NoError(t, err)
	assert.Equal(t, "95555@message.cmbchina.com", message.Sender)
	assert.Equal(t, "招商银行通知", message.Subject)
	assert.Equal(t, "<multipart-bill@example.com>", message.MessageID)
	assert.Equal(t, "2026-09-07T09:00:00+08:00", message.ReceivedAt.Format(time.RFC3339))
	assert.True(t, message.Authenticated)
	assert.Empty(t, message.Text)

	// Full-message decoding must still reject a truncated multipart body.
	_, err = DecodeMessage(strings.NewReader(raw), security, time.Time{})
	require.ErrorContains(t, err, "read MIME part")
}

func TestDecodeMessageHeadersPreservesAuthenticationAndSizeChecks(t *testing.T) {
	raw := "From: 95555@message.cmbchina.com\r\nSubject: Bill\r\nContent-Type: multipart/alternative; boundary=bill\r\n\r\n"
	security := MessageSecurity{RequireAuthenticationResults: true, TrustedAuthservDomains: []string{"qq.com"}}

	message, err := DecodeMessageHeadersWithLimit(strings.NewReader(raw), security, time.Now(), DefaultMaxMessageBytes)
	require.NoError(t, err)
	assert.False(t, message.Authenticated)

	_, err = DecodeMessageHeadersWithLimit(strings.NewReader(raw), security, time.Now(), 32)
	require.ErrorContains(t, err, "size limit")
}
