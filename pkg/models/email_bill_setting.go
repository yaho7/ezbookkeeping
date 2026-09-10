package models

// EmailBillSettingsResponse contains the editable email bill importer settings.
type EmailBillSettingsResponse struct {
	FolderMode            string   `json:"folderMode"`
	Folders               []string `json:"folders"`
	Enabled               bool     `json:"enabled"`
	IMAPServer            string   `json:"imapServer"`
	IMAPPort              uint16   `json:"imapPort"`
	MailUser              string   `json:"mailUser"`
	PasswordConfigured    bool     `json:"passwordConfigured"`
	Timezone              string   `json:"timezone"`
	CronExpression        string   `json:"cronExpression"`
	MaxEmails             uint32   `json:"maxEmails"`
	RetainRawEmails       bool     `json:"retainRawEmails"`
	RawEmailRetentionDays uint32   `json:"rawEmailRetentionDays"`
}

// EmailBillSettingsUpdateRequest contains editable email bill importer settings.
type EmailBillSettingsUpdateRequest struct {
	FolderMode            string   `json:"folderMode"`
	Folders               []string `json:"folders" binding:"max=200,dive,max=1000"`
	Enabled               bool     `json:"enabled"`
	IMAPServer            string   `json:"imapServer"`
	IMAPPort              uint16   `json:"imapPort"`
	MailUser              string   `json:"mailUser"`
	MailPassword          string   `json:"mailPassword"`
	Timezone              string   `json:"timezone"`
	CronExpression        string   `json:"cronExpression"`
	MaxEmails             uint32   `json:"maxEmails"`
	RetainRawEmails       bool     `json:"retainRawEmails"`
	RawEmailRetentionDays uint32   `json:"rawEmailRetentionDays"`
}
