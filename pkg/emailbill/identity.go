package emailbill

import (
	"crypto/sha256"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	MessageFingerprintVersion uint16 = 1
	CandidateIdentityVersion  uint16 = 1
	BillFingerprintVersion    uint16 = 1
)

// MessageIdentityInput contains stable fields used to identify one logical email.
type MessageIdentityInput struct {
	MailboxID  int64
	MessageID  string
	Sender     string
	Subject    string
	ReceivedAt time.Time
	Body       string
}

// CandidateIdentityInput contains fields used to identify one logical bill.
type CandidateIdentityInput struct {
	UserID             int64
	Bank               string
	AccountLast4       string
	ExternalID         string
	MessageFingerprint string
	BillSequence       int
}

// BillFingerprintInput contains the exact parsed representation of a bill.
type BillFingerprintInput struct {
	IdentityKey     string
	TransactionTime time.Time
	AmountMinor     int64
	Currency        string
	Direction       string
	Merchant        string
	Description     string
}

// MessageFingerprint returns a versioned fingerprint for a logical email.
func MessageFingerprint(input MessageIdentityInput) (string, uint16) {
	messageID := strings.ToLower(strings.TrimSpace(input.MessageID))
	if messageID != "" {
		return hashIdentity("message-id", strconv.FormatInt(input.MailboxID, 10), messageID), MessageFingerprintVersion
	}

	body := strings.ReplaceAll(input.Body, "\r\n", "\n")
	body = strings.ReplaceAll(body, "\r", "\n")
	return hashIdentity(
		"fallback",
		strconv.FormatInt(input.MailboxID, 10),
		normalizeText(input.Sender),
		normalizeText(input.Subject),
		input.ReceivedAt.UTC().Format(time.RFC3339Nano),
		strings.TrimSpace(body),
	), MessageFingerprintVersion
}

// CandidateIdentity returns a key that excludes mutable parser interpretations.
func CandidateIdentity(input CandidateIdentityInput) (string, uint16) {
	externalID := strings.TrimSpace(input.ExternalID)
	if externalID != "" {
		return hashIdentity(
			"external",
			strconv.FormatInt(input.UserID, 10),
			normalizeText(input.Bank),
			strings.TrimSpace(input.AccountLast4),
			externalID,
		), CandidateIdentityVersion
	}

	return hashIdentity(
		"message-sequence",
		strconv.FormatInt(input.UserID, 10),
		strings.TrimSpace(input.MessageFingerprint),
		strconv.Itoa(input.BillSequence),
	), CandidateIdentityVersion
}

// BillFingerprint returns a key for an exact parsed bill representation.
func BillFingerprint(input BillFingerprintInput) (string, uint16) {
	return hashIdentity(
		"bill",
		strings.TrimSpace(input.IdentityKey),
		input.TransactionTime.UTC().Format(time.RFC3339Nano),
		strconv.FormatInt(input.AmountMinor, 10),
		strings.ToUpper(strings.TrimSpace(input.Currency)),
		normalizeText(input.Direction),
		normalizeText(input.Merchant),
		normalizeText(input.Description),
	), BillFingerprintVersion
}

func normalizeText(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(value)), " "))
}

func hashIdentity(parts ...string) string {
	digest := sha256.Sum256([]byte(strings.Join(parts, "\x1f")))
	return fmt.Sprintf("sha256:%x", digest[:])
}
