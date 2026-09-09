package settings

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/ini.v1"
)

func TestSaveEmailBillConfigurationPreservesOtherSections(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "ezbookkeeping.ini")
	require.NoError(t, os.WriteFile(configPath, []byte("[server]\nhttp_port = 8080\n\n[email_bill]\nenabled = false\n"), 0o600))

	emailConfig := &EmailBillConfig{
		Enabled:                      true,
		TargetUser:                   "alice",
		IMAPServer:                   "imap.qq.com",
		IMAPPort:                     993,
		MailUser:                     "alice@qq.com",
		MailPassword:                 "app-password",
		CMBCreditAccountID:           101,
		CMBDebitAccountID:            102,
		ExpenseCategoryID:            201,
		IncomeCategoryID:             202,
		Timezone:                     "Asia/Shanghai",
		CronExpression:               "30 8 * * 1-5",
		MaxEmails:                    60,
		MaxMessageBytes:              2 * 1024 * 1024,
		RequireAuthenticationResults: true,
		TrustedAuthservDomains:       []string{"qq.com"},
	}

	require.NoError(t, SaveEmailBillConfiguration(configPath, emailConfig))

	configFile, err := ini.Load(configPath)
	require.NoError(t, err)
	assert.Equal(t, "8080", configFile.Section("server").Key("http_port").String())
	assert.Equal(t, "true", configFile.Section("email_bill").Key("enabled").String())
	assert.Equal(t, "alice", configFile.Section("email_bill").Key("target_user").String())
	assert.Equal(t, "app-password", configFile.Section("email_bill").Key("mail_password").String())
	assert.Equal(t, "30 8 * * 1-5", configFile.Section("email_bill").Key("cron_expression").String())
	assert.Equal(t, "qq.com", configFile.Section("email_bill").Key("trusted_authserv_domains").String())

	fileInfo, err := os.Stat(configPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), fileInfo.Mode().Perm())
}

func TestLoadEmailBillConfigurationDisabledByDefault(t *testing.T) {
	config := &Config{}

	err := loadEmailBillConfiguration(config, ini.Empty(), "email_bill")

	require.NoError(t, err)
	require.NotNil(t, config.EmailBillConfig)
	assert.False(t, config.EmailBillConfig.Enabled)
	assert.False(t, config.EmailBillConfig.RetainRawEmails)
	assert.Equal(t, uint32(30), config.EmailBillConfig.RawEmailRetentionDays)
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
	assert.Equal(t, "0 3 * * *", actual.CronExpression)
	assert.Equal(t, uint32(60), actual.MaxEmails)
	assert.Equal(t, uint32(2*1024*1024), actual.MaxMessageBytes)
	assert.True(t, actual.RequireAuthenticationResults)
	assert.Equal(t, []string{"qq.com"}, actual.TrustedAuthservDomains)
	assert.Equal(t, int64(101), actual.CMBCreditAccountID)
	assert.Equal(t, int64(202), actual.IncomeCategoryID)
}

func TestLoadEmailBillConfigurationRejectsInvalidCronExpression(t *testing.T) {
	configFile, err := ini.Load([]byte(`[email_bill]
enabled = true
target_user = alice
mail_user = alice@qq.com
mail_password = app-password
cmb_credit_account_id = 101
cmb_debit_account_id = 102
expense_category_id = 201
income_category_id = 202
cron_expression = every morning
`))
	require.NoError(t, err)

	err = loadEmailBillConfiguration(&Config{}, configFile, "email_bill")

	require.ErrorContains(t, err, "cron_expression")
}

func TestLoadEmailBillConfigurationRejectsCronTimezonePrefix(t *testing.T) {
	configFile, err := ini.Load([]byte(`[email_bill]
enabled = true
target_user = alice
mail_user = alice@qq.com
mail_password = app-password
cmb_credit_account_id = 101
cmb_debit_account_id = 102
expense_category_id = 201
income_category_id = 202
cron_expression = CRON_TZ=UTC 0 3 * * *
`))
	require.NoError(t, err)

	err = loadEmailBillConfiguration(&Config{}, configFile, "email_bill")

	require.ErrorContains(t, err, "five fields")
}

func TestDefaultAuthenticationServiceDomains(t *testing.T) {
	assert.Equal(t, []string{"google.com"}, defaultAuthenticationServiceDomains("gmail.com", "imap.gmail.com"))
	assert.Equal(t, []string{"outlook.com"}, defaultAuthenticationServiceDomains("hotmail.com", "outlook.office365.com"))
	assert.Equal(t, []string{"imap.example.com"}, defaultAuthenticationServiceDomains("example.com", "imap.example.com"))
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

func TestNormalizeEmailBillConfigurationDoesNotRequireFixedMappings(t *testing.T) {
	config := &EmailBillConfig{
		Enabled: true, TargetUser: "alice", IMAPServer: "imap.example.com", IMAPPort: 993,
		MailUser: "alice@example.com", MailPassword: "secret", Timezone: "Asia/Shanghai",
		CronExpression: "30 8 * * 1-5", MaxEmails: 60, MaxMessageBytes: 2 * 1024 * 1024,
		RequireAuthenticationResults: true, TrustedAuthservDomains: []string{"example.com"},
	}

	assert.NoError(t, NormalizeEmailBillConfiguration(config))
}
