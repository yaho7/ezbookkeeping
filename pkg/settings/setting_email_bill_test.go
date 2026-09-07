package settings

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/ini.v1"
)

func TestLoadEmailBillConfigurationDisabledByDefault(t *testing.T) {
	config := &Config{}

	err := loadEmailBillConfiguration(config, ini.Empty(), "email_bill")

	require.NoError(t, err)
	require.NotNil(t, config.EmailBillConfig)
	assert.False(t, config.EmailBillConfig.Enabled)
}

func TestLoadEmailBillConfigurationInfersIMAPServer(t *testing.T) {
	configFile, err := ini.Load([]byte(`[email_bill]
enabled = true
target_user = alice
mail_user = alice@qq.com
mail_password = app-password
cmb_credit_account_id = 101
cmb_debit_account_id = 102
expense_category_id = 201
income_category_id = 202
`))
	require.NoError(t, err)

	config := &Config{}
	err = loadEmailBillConfiguration(config, configFile, "email_bill")

	require.NoError(t, err)
	actual := config.EmailBillConfig
	require.NotNil(t, actual)
	assert.True(t, actual.Enabled)
	assert.Equal(t, "imap.qq.com", actual.IMAPServer)
	assert.Equal(t, uint16(993), actual.IMAPPort)
	assert.Equal(t, "Asia/Shanghai", actual.Timezone)
	assert.Equal(t, time.Hour, actual.IntervalDuration)
	assert.Equal(t, uint32(60), actual.MaxEmails)
	assert.True(t, actual.RequireAuthenticationResults)
	assert.Equal(t, []string{"qq.com"}, actual.TrustedAuthservDomains)
	assert.Equal(t, int64(101), actual.CMBCreditAccountID)
	assert.Equal(t, int64(202), actual.IncomeCategoryID)
}

func TestLoadEmailBillConfigurationRejectsMissingRequiredValue(t *testing.T) {
	configFile, err := ini.Load([]byte(`[email_bill]
enabled = true
mail_user = alice@example.com
`))
	require.NoError(t, err)

	err = loadEmailBillConfiguration(&Config{}, configFile, "email_bill")

	require.ErrorContains(t, err, "target_user")
}
