package models

// EmailBillSettingsResponse contains the editable email bill importer settings.
type EmailBillSettingsResponse struct {
	Enabled                      bool     `json:"enabled"`
	IMAPServer                   string   `json:"imapServer"`
	IMAPPort                     uint16   `json:"imapPort"`
	MailUser                     string   `json:"mailUser"`
	PasswordConfigured           bool     `json:"passwordConfigured"`
	Timezone                     string   `json:"timezone"`
	CronExpression               string   `json:"cronExpression"`
	MaxEmails                    uint32   `json:"maxEmails"`
	RequireAuthenticationResults bool     `json:"requireAuthenticationResults"`
	TrustedAuthservDomains       []string `json:"trustedAuthservDomains"`
	RetainRawEmails              bool     `json:"retainRawEmails"`
	RawEmailRetentionDays        uint32   `json:"rawEmailRetentionDays"`
}

// EmailBillSettingsUpdateRequest contains editable email bill importer settings.
type EmailBillSettingsUpdateRequest struct {
	Enabled                      bool     `json:"enabled"`
	IMAPServer                   string   `json:"imapServer"`
	IMAPPort                     uint16   `json:"imapPort"`
	MailUser                     string   `json:"mailUser"`
	MailPassword                 string   `json:"mailPassword"`
	Timezone                     string   `json:"timezone"`
	CronExpression               string   `json:"cronExpression"`
	MaxEmails                    uint32   `json:"maxEmails"`
	RequireAuthenticationResults bool     `json:"requireAuthenticationResults"`
	TrustedAuthservDomains       []string `json:"trustedAuthservDomains"`
	RetainRawEmails              bool     `json:"retainRawEmails"`
	RawEmailRetentionDays        uint32   `json:"rawEmailRetentionDays"`
}
