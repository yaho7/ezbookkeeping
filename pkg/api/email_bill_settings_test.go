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
