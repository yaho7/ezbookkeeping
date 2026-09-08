package models

// EmailBillSettingsResponse contains the editable email bill importer settings.
type EmailBillSettingsResponse struct {
	Enabled                      bool     `json:"enabled"`
	IMAPServer                   string   `json:"imapServer"`
	IMAPPort                     uint16   `json:"imapPort"`
	MailUser                     string   `json:"mailUser"`
	PasswordConfigured           bool     `json:"passwordConfigured"`
	CMBCreditAccountID           int64    `json:"cmbCreditAccountId,string"`
	CMBDebitAccountID            int64    `json:"cmbDebitAccountId,string"`
	ExpenseCategoryID            int64    `json:"expenseCategoryId,string"`
	IncomeCategoryID             int64    `json:"incomeCategoryId,string"`
	Timezone                     string   `json:"timezone"`
	CronExpression               string   `json:"cronExpression"`
	MaxEmails                    uint32   `json:"maxEmails"`
	RequireAuthenticationResults bool     `json:"requireAuthenticationResults"`
	TrustedAuthservDomains       []string `json:"trustedAuthservDomains"`
}

// EmailBillSettingsUpdateRequest contains editable email bill importer settings.
type EmailBillSettingsUpdateRequest struct {
	Enabled                      bool     `json:"enabled"`
	IMAPServer                   string   `json:"imapServer"`
	IMAPPort                     uint16   `json:"imapPort"`
	MailUser                     string   `json:"mailUser"`
	MailPassword                 string   `json:"mailPassword"`
	CMBCreditAccountID           int64    `json:"cmbCreditAccountId,string"`
	CMBDebitAccountID            int64    `json:"cmbDebitAccountId,string"`
	ExpenseCategoryID            int64    `json:"expenseCategoryId,string"`
	IncomeCategoryID             int64    `json:"incomeCategoryId,string"`
	Timezone                     string   `json:"timezone"`
	CronExpression               string   `json:"cronExpression"`
	MaxEmails                    uint32   `json:"maxEmails"`
	RequireAuthenticationResults bool     `json:"requireAuthenticationResults"`
	TrustedAuthservDomains       []string `json:"trustedAuthservDomains"`
}
