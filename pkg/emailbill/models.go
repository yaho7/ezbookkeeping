package emailbill

import "time"

// Source identifies the account mapping used for an imported bill transaction.
type Source string

const (
	SourceCMBCredit Source = "cmb_credit"
	SourceCMBDebit  Source = "cmb_debit"
)

// Message contains the normalized parts of one matching email.
type Message struct {
	Fingerprint   string
	MessageID     string
	Sender        string
	Subject       string
	ReceivedAt    time.Time
	Text          string
	Headers       map[string]string
	Authenticated bool
}

// ParsedTransaction is a bank transaction before ezBookkeeping account mapping.
type ParsedTransaction struct {
	Source      Source
	OccurredAt  time.Time
	AmountMinor int64
	Merchant    string
	Description string
}

// Parser recognizes and parses one supported bank email format.
type Parser interface {
	Matches(sender string, subject string) bool
	Parse(text string, receivedAt time.Time) ([]ParsedTransaction, error)
	AllowedSenders() []string
	SubjectKeywords() []string
}
