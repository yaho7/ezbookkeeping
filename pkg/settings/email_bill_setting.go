package settings

import (
	"encoding/json"
	"strconv"

	"gopkg.in/ini.v1"
)

// SaveEmailBillConfiguration atomically persists the email bill section while preserving all other settings.
func SaveEmailBillConfiguration(configFilePath string, config *EmailBillConfig) error {
	return updateConfigurationFile(configFilePath, func(configFile *ini.File) error {

		section, err := configFile.NewSection("email_bill")
		if err != nil {
			return err
		}

		folders, err := json.Marshal(config.Folders)
		if err != nil {
			return err
		}
		values := map[string]string{
			"folder_mode":              config.FolderMode,
			"folders":                  string(folders),
			"enabled":                  strconv.FormatBool(config.Enabled),
			"target_user":              config.TargetUser,
			"imap_server":              config.IMAPServer,
			"imap_port":                strconv.FormatUint(uint64(config.IMAPPort), 10),
			"mail_user":                config.MailUser,
			"mail_password":            config.MailPassword,
			"timezone":                 config.Timezone,
			"cron_expression":          config.CronExpression,
			"max_emails":               strconv.FormatUint(uint64(config.MaxEmails), 10),
			"max_message_bytes":        strconv.FormatUint(uint64(config.MaxMessageBytes), 10),
			"retain_raw_emails":        strconv.FormatBool(config.RetainRawEmails),
			"raw_email_retention_days": strconv.FormatUint(uint64(config.RawEmailRetentionDays), 10),
		}
		if n := config.Notification; n != nil {
			values["notification_enabled"] = strconv.FormatBool(n.Enabled)
			values["notification_mode"] = n.Mode
			values["notification_recipient"] = n.Recipient
			values["notification_smtp_server"] = n.SMTPServer
			values["notification_smtp_port"] = strconv.FormatUint(uint64(n.SMTPPort), 10)
			values["notification_smtp_user"] = n.SMTPUser
			values["notification_smtp_password"] = n.SMTPPassword
			values["notification_from_address"] = n.FromAddress
			values["notification_from_name"] = n.FromName
			values["notification_subject"] = n.Subject
			values["notification_use_mailbox_credentials"] = strconv.FormatBool(n.UseMailboxCredentials)
		}
		for key, value := range values {
			section.Key(key).SetValue(value)
		}
		for _, legacyKey := range []string{"cmb_credit_account_id", "cmb_debit_account_id", "expense_category_id", "income_category_id"} {
			section.DeleteKey(legacyKey)
		}
		section.DeleteKey("require_authentication_results")
		section.DeleteKey("trusted_authserv_domains")

		return nil
	})
}
