package models

// EmailBillSettingsResponse contains the editable email bill importer settings.
type EmailBillSettingsResponse struct {
	Notification          *EmailBillNotificationSettingsResponse `json:"notification"`
	FolderMode            string                                 `json:"folderMode"`
	Folders               []string                               `json:"folders"`
	Enabled               bool                                   `json:"enabled"`
	IMAPServer            string                                 `json:"imapServer"`
	IMAPPort              uint16                                 `json:"imapPort"`
	MailUser              string                                 `json:"mailUser"`
	PasswordConfigured    bool                                   `json:"passwordConfigured"`
	Timezone              string                                 `json:"timezone"`
	CronExpression        string                                 `json:"cronExpression"`
	MaxEmails             uint32                                 `json:"maxEmails"`
	RetainRawEmails       bool                                   `json:"retainRawEmails"`
	RawEmailRetentionDays uint32                                 `json:"rawEmailRetentionDays"`
}

// EmailBillSettingsUpdateRequest contains editable email bill importer settings.
type EmailBillSettingsUpdateRequest struct {
	Notification          *EmailBillNotificationSettingsRequest `json:"notification"`
	FolderMode            string                                `json:"folderMode"`
	Folders               []string                              `json:"folders" binding:"max=200,dive,max=1000"`
	Enabled               bool                                  `json:"enabled"`
	IMAPServer            string                                `json:"imapServer"`
	IMAPPort              uint16                                `json:"imapPort"`
	MailUser              string                                `json:"mailUser"`
	MailPassword          string                                `json:"mailPassword"`
	Timezone              string                                `json:"timezone"`
	CronExpression        string                                `json:"cronExpression"`
	MaxEmails             uint32                                `json:"maxEmails"`
	RetainRawEmails       bool                                  `json:"retainRawEmails"`
	RawEmailRetentionDays uint32                                `json:"rawEmailRetentionDays"`
}

type EmailBillNotificationSettingsResponse struct {
	Enabled               bool   `json:"enabled"`
	Mode                  string `json:"mode"`
	Recipient             string `json:"recipient"`
	SMTPServer            string `json:"smtpServer"`
	SMTPPort              uint16 `json:"smtpPort"`
	SMTPUser              string `json:"smtpUser"`
	PasswordConfigured    bool   `json:"passwordConfigured"`
	FromAddress           string `json:"fromAddress"`
	FromName              string `json:"fromName"`
	Subject               string `json:"subject"`
	UseMailboxCredentials bool   `json:"useMailboxCredentials"`
}

type EmailBillNotificationSettingsRequest struct {
	Enabled               bool   `json:"enabled"`
	Mode                  string `json:"mode"`
	Recipient             string `json:"recipient" binding:"max=254"`
	SMTPServer            string `json:"smtpServer" binding:"max=255"`
	SMTPPort              uint16 `json:"smtpPort"`
	SMTPUser              string `json:"smtpUser" binding:"max=255"`
	SMTPPassword          string `json:"smtpPassword" binding:"max=4096"`
	FromAddress           string `json:"fromAddress" binding:"max=254"`
	FromName              string `json:"fromName" binding:"max=400"`
	Subject               string `json:"subject" binding:"max=480"`
	UseMailboxCredentials bool   `json:"useMailboxCredentials"`
}
