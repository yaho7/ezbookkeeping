package settings

import (
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestEmailBillNotificationDefaultsAreDisabledAndInferFoxmail(t *testing.T) {
	c := &EmailBillConfig{MailUser: "alice@foxmail.com", MailPassword: "mail-secret"}
	require.NoError(t, NormalizeEmailBillNotification(c, false))
	assert.False(t, c.Notification.Enabled)
	assert.Equal(t, "always", c.Notification.Mode)
	assert.Equal(t, "smtp.qq.com", c.Notification.SMTPServer)
	assert.Equal(t, uint16(465), c.Notification.SMTPPort)
	assert.Equal(t, c.MailUser, c.Notification.Recipient)
	assert.True(t, c.Notification.UseMailboxCredentials)
	assert.Equal(t, "ezBookkeeping", c.Notification.FromName)
	c.Notification.Enabled = true
	require.NoError(t, NormalizeEmailBillNotification(c, false))
	assert.Equal(t, c.MailPassword, EmailBillNotificationSMTP(c).SMTPPasswd)
	assert.Equal(t, "ezBookkeeping", EmailBillNotificationSMTP(c).FromName)
}

func TestEmailBillNotificationRejectsHeaderInjectionAndMissingCredentials(t *testing.T) {
	for _, recipient := range []string{"alice@example.com\r\nBcc: other@example.com", "alice@example.com,other@example.com", "not-an-email"} {
		c := &EmailBillConfig{MailUser: "alice@qq.com", MailPassword: "secret", Notification: &EmailBillNotificationConfig{Enabled: true, Mode: "always", Recipient: recipient, UseMailboxCredentials: true}}
		require.ErrorContains(t, NormalizeEmailBillNotification(c, false), "recipient")
	}
	c := &EmailBillConfig{MailUser: "alice@qq.com", Notification: &EmailBillNotificationConfig{Enabled: true, UseMailboxCredentials: true}}
	require.ErrorContains(t, NormalizeEmailBillNotification(c, false), "mailbox password")
	c.Notification.UseMailboxCredentials = false
	require.ErrorContains(t, NormalizeEmailBillNotification(c, false), "SMTP password")
	c.Notification.UseMailboxCredentials, c.Notification.FromName = true, "账单\r\nBcc: attacker@example.com"
	c.MailPassword = "secret"
	require.ErrorContains(t, NormalizeEmailBillNotification(c, false), "sender name")
}
