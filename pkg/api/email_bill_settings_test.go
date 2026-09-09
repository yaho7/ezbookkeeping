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
	current := &settings.EmailBillConfig{MailPassword: "stored-password"}
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
