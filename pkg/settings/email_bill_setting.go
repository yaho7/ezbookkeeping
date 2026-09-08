package settings

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/ini.v1"
)

// SaveEmailBillConfiguration atomically persists the email bill section while preserving all other settings.
func SaveEmailBillConfiguration(configFilePath string, config *EmailBillConfig) error {
	configFile, err := ini.LoadSources(ini.LoadOptions{IgnoreInlineComment: true}, configFilePath)
	if err != nil {
		return err
	}

	section, err := configFile.NewSection("email_bill")
	if err != nil {
		return err
	}

	values := map[string]string{
		"enabled":                        strconv.FormatBool(config.Enabled),
		"target_user":                    config.TargetUser,
		"imap_server":                    config.IMAPServer,
		"imap_port":                      strconv.FormatUint(uint64(config.IMAPPort), 10),
		"mail_user":                      config.MailUser,
		"mail_password":                  config.MailPassword,
		"timezone":                       config.Timezone,
		"cron_expression":                config.CronExpression,
		"max_emails":                     strconv.FormatUint(uint64(config.MaxEmails), 10),
		"max_message_bytes":              strconv.FormatUint(uint64(config.MaxMessageBytes), 10),
		"require_authentication_results": strconv.FormatBool(config.RequireAuthenticationResults),
		"trusted_authserv_domains":       strings.Join(config.TrustedAuthservDomains, ","),
	}
	for key, value := range values {
		section.Key(key).SetValue(value)
	}
	for _, legacyKey := range []string{"cmb_credit_account_id", "cmb_debit_account_id", "expense_category_id", "income_category_id"} {
		section.DeleteKey(legacyKey)
	}

	fileInfo, err := os.Stat(configFilePath)
	if err != nil {
		return err
	}
	tempFile, err := os.CreateTemp(filepath.Dir(configFilePath), ".ezbookkeeping-*.ini")
	if err != nil {
		return err
	}
	tempPath := tempFile.Name()
	if err = tempFile.Close(); err != nil {
		return err
	}
	defer os.Remove(tempPath)

	if err = configFile.SaveTo(tempPath); err != nil {
		return err
	}
	if err = os.Chmod(tempPath, fileInfo.Mode().Perm()); err != nil {
		return err
	}
	return os.Rename(tempPath, configFilePath)
}
