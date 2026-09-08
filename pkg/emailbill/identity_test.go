package emailbill

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMessageFingerprintPrefersNormalizedMessageID(t *testing.T) {
	input := MessageIdentityInput{
		MailboxID: 7,
		MessageID: "  <Bill-42@Example.COM>  ",
		Sender:    "ignored@example.com",
		Subject:   "ignored",
		Body:      "ignored",
	}

	fingerprint, version := MessageFingerprint(input)
	normalizedFingerprint, _ := MessageFingerprint(MessageIdentityInput{MailboxID: 7, MessageID: "<bill-42@example.com>"})

	assert.Equal(t, MessageFingerprintVersion, version)
	assert.Equal(t, normalizedFingerprint, fingerprint)
	assert.NotEmpty(t, fingerprint)
}

func TestMessageFingerprintFallbackNormalizesEquivalentMail(t *testing.T) {
	receivedAt := time.Date(2026, 9, 8, 10, 30, 0, 0, time.FixedZone("CST", 8*60*60))
	left, leftVersion := MessageFingerprint(MessageIdentityInput{
		MailboxID:  7,
		Sender:     " Bank@Example.COM ",
		Subject:    "  消费   通知 ",
		ReceivedAt: receivedAt,
		Body:       "商户： 麦当劳\r\n金额： 20.00",
	})
	right, rightVersion := MessageFingerprint(MessageIdentityInput{
		MailboxID:  7,
		Sender:     "bank@example.com",
		Subject:    "消费 通知",
		ReceivedAt: receivedAt.UTC(),
		Body:       "商户： 麦当劳\n金额： 20.00",
	})

	assert.Equal(t, MessageFingerprintVersion, leftVersion)
	assert.Equal(t, leftVersion, rightVersion)
	assert.Equal(t, left, right)
}

func TestCandidateIdentityUsesExternalIDWhenAvailable(t *testing.T) {
	base := CandidateIdentityInput{
		UserID:             11,
		Bank:               "CMB",
		AccountLast4:       " 5460 ",
		ExternalID:         " flow-123 ",
		MessageFingerprint: "mail-one",
		BillSequence:       1,
	}
	changedMail := base
	changedMail.MessageFingerprint = "mail-two"
	changedMail.BillSequence = 99

	left, version := CandidateIdentity(base)
	right, _ := CandidateIdentity(changedMail)

	assert.Equal(t, CandidateIdentityVersion, version)
	assert.Equal(t, left, right)
}

func TestCandidateIdentityFallsBackToMessageAndSequence(t *testing.T) {
	first, _ := CandidateIdentity(CandidateIdentityInput{UserID: 11, MessageFingerprint: "mail-one", BillSequence: 1})
	second, _ := CandidateIdentity(CandidateIdentityInput{UserID: 11, MessageFingerprint: "mail-one", BillSequence: 2})

	assert.NotEqual(t, first, second)
}

func TestBillFingerprintIncludesParsedFieldsButCandidateIdentityDoesNot(t *testing.T) {
	identity, _ := CandidateIdentity(CandidateIdentityInput{
		UserID: 11, Bank: "cmb", AccountLast4: "5460", ExternalID: "flow-123",
	})
	timeA := time.Date(2026, 9, 8, 10, 30, 0, 0, time.UTC)
	base := BillFingerprintInput{
		IdentityKey:     identity,
		TransactionTime: timeA,
		AmountMinor:     -2000,
		Currency:        "CNY",
		Merchant:        "麦当劳",
		Description:     "午餐",
	}

	first, version := BillFingerprint(base)
	changed := base
	changed.AmountMinor = -20000
	second, _ := BillFingerprint(changed)

	require.NotEmpty(t, first)
	assert.Equal(t, BillFingerprintVersion, version)
	assert.NotEqual(t, first, second)
}
