package api

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mayswind/ezbookkeeping/pkg/models"
	"github.com/mayswind/ezbookkeeping/pkg/settings"
)

func TestBuildEmailBillConfigPreservesStoredPassword(t *testing.T) {
	current := &settings.EmailBillConfig{
		IMAPServer: "imap.qq.com", IMAPPort: 993,
		MailUser: "alice@qq.com", MailPassword: "stored-password",
	}
	request := &models.EmailBillSettingsUpdateRequest{
		Enabled:        true,
		IMAPServer:     "imap.qq.com",
		IMAPPort:       993,
		MailUser:       "alice@qq.com",
		Timezone:       "Asia/Shanghai",
		CronExpression: "30 8 * * 1-5",
		MaxEmails:      60,
	}

	actual, err := buildEmailBillConfig("alice", request, current)

	require.NoError(t, err)
	assert.Equal(t, "alice", actual.TargetUser)
	assert.Equal(t, "stored-password", actual.MailPassword)
	assert.Equal(t, uint32(2*1024*1024), actual.MaxMessageBytes)
}

func TestBuildEmailBillConfigAllowsDisablingWithoutCredentials(t *testing.T) {
	actual, err := buildEmailBillConfig("alice", &models.EmailBillSettingsUpdateRequest{Enabled: false}, nil)

	require.NoError(t, err)
	assert.False(t, actual.Enabled)
	assert.Equal(t, "alice", actual.TargetUser)
}

func TestEmailBillNotificationSecretsStayOnServerAndOldClientsPreserveSettings(t *testing.T) {
	current := &settings.EmailBillConfig{MailUser: "alice@qq.com", MailPassword: "mail-secret", Notification: &settings.EmailBillNotificationConfig{Mode: "always", SMTPServer: "smtp.qq.com", SMTPPort: 465, SMTPUser: "alice@qq.com", SMTPPassword: "smtp-secret", FromName: "账单助手", Subject: "旧主题"}}
	response, err := json.Marshal(emailBillSettingsResponse(current))
	require.NoError(t, err)
	assert.NotContains(t, string(response), "mail-secret")
	assert.NotContains(t, string(response), "smtp-secret")
	assert.Contains(t, string(response), "passwordConfigured")
	assert.Contains(t, string(response), "\"fromName\":\"账单助手\"")
	assert.Contains(t, string(response), "\"subject\":\"旧主题\"")
	updated, err := buildEmailBillConfig("alice", &models.EmailBillSettingsUpdateRequest{MailUser: "alice@qq.com"}, current)
	require.NoError(t, err)
	assert.Equal(t, "smtp-secret", updated.Notification.SMTPPassword)
	assert.Equal(t, "账单助手", updated.Notification.FromName)
	assert.Equal(t, "旧主题", updated.Notification.Subject)
	updated.Notification.Mode = "errors_only"
	assert.Equal(t, "always", current.Notification.Mode)
	request := &models.EmailBillNotificationSettingsRequest{SMTPServer: "smtp.qq.com", SMTPPort: 465, SMTPUser: "alice@qq.com", FromName: "新的名称", Subject: "新的主题"}
	assert.Equal(t, "smtp-secret", buildEmailBillNotificationConfig(request, current.Notification).SMTPPassword)
	assert.Equal(t, "新的名称", buildEmailBillNotificationConfig(request, current.Notification).FromName)
	assert.Equal(t, "新的主题", buildEmailBillNotificationConfig(request, current.Notification).Subject)
	request.SMTPServer = "smtp.other.example"
	assert.Empty(t, buildEmailBillNotificationConfig(request, current.Notification).SMTPPassword)
}

func TestEmailBillSettingsResponseOmitsAuthenticationOptions(t *testing.T) {
	raw, err := json.Marshal(emailBillSettingsResponse(&settings.EmailBillConfig{}))

	require.NoError(t, err)
	assert.NotContains(t, string(raw), "requireAuthenticationResults")
	assert.NotContains(t, string(raw), "trustedAuthservDomains")
}

func TestBuildEmailBillConfigRequiresNewPasswordWhenMailboxChanges(t *testing.T) {
	for _, item := range []struct {
		name   string
		server string
		port   uint16
		user   string
	}{
		{"server", "imap.example.com", 993, "alice@qq.com"},
		{"port", "imap.qq.com", 1993, "alice@qq.com"},
		{"user", "imap.qq.com", 993, "bob@qq.com"},
	} {
		t.Run(item.name, func(t *testing.T) {
			current := &settings.EmailBillConfig{IMAPServer: "imap.qq.com", IMAPPort: 993, MailUser: "alice@qq.com", MailPassword: "stored-password"}
			request := &models.EmailBillSettingsUpdateRequest{
				Enabled: true, IMAPServer: item.server, IMAPPort: item.port, MailUser: item.user,
				Timezone: "Asia/Shanghai", CronExpression: "30 8 * * 1-5", MaxEmails: 60,
			}
			_, err := buildEmailBillConfig("alice", request, current)
			require.ErrorContains(t, err, "mail_password is required")

			request.MailPassword = "replacement-password"
			actual, err := buildEmailBillConfig("alice", request, current)
			require.NoError(t, err)
			assert.Equal(t, "replacement-password", actual.MailPassword)
			assert.Equal(t, "stored-password", current.MailPassword)
		})
	}
}
