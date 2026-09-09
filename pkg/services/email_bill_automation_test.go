package services

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mayswind/ezbookkeeping/pkg/emailbill"
)

func TestEmailBillParserTestBenchReturnsPreviewWithoutPersistence(t *testing.T) {
	service := NewEmailBillAutomationService(nil, nil)
	request := EmailBillParserTestRequest{
		UID:        7,
		Matcher:    emailbill.ParserMatcher{Senders: []string{"bank@example.com"}},
		SourceCode: oneBillScript("coffee"),
		Mail: EmailBillFetchedMessage{
			RemoteMessageID: "<preview@example.com>", Sender: "bank@example.com", Subject: "bill",
			ReceivedAt: time.Date(2026, 9, 8, 10, 0, 0, 0, time.UTC), Text: "sample", Authenticated: true,
		},
	}

	preview, err := service.TestParser(request)
	require.NoError(t, err)
	require.True(t, preview.Matched)
	require.Len(t, preview.Bills, 1)
	assert.Equal(t, "coffee", preview.Bills[0].Bill.Merchant)
	assert.Contains(t, preview.Bills[0].CandidateIdentity, "sha256:")
	assert.Contains(t, preview.Bills[0].BillFingerprint, "sha256:")
}

func TestEmailBillParserTestBenchDoesNotRunWhenMatcherMisses(t *testing.T) {
	service := NewEmailBillAutomationService(nil, nil)
	preview, err := service.TestParser(EmailBillParserTestRequest{
		UID: 7, Matcher: emailbill.ParserMatcher{Senders: []string{"other@example.com"}},
		SourceCode: "invalid source is intentionally never executed",
		Mail:       authenticatedScriptMail(),
	})

	require.NoError(t, err)
	assert.False(t, preview.Matched)
	assert.Empty(t, preview.Bills)
}

func TestEmailBillParserTestBenchRejectsInvalidParserOutput(t *testing.T) {
	service := NewEmailBillAutomationService(nil, nil)
	_, err := service.TestParser(EmailBillParserTestRequest{
		UID: 7, SourceCode: "def parse(mail):\n  return [{\"amount\": \"1.00\"}]",
		Mail: authenticatedScriptMail(),
	})

	assert.ErrorContains(t, err, "occurred_at")
}

func TestEmailBillClassificationRuleValidationRejectsInvalidRegex(t *testing.T) {
	err := validateEmailBillClassificationRule(EmailBillClassificationRuleInput{
		Origin: "manual", MerchantPattern: "[", MatchType: "regex", CategoryID: 8,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "regex")
}

func TestEmailBillRoutingRuleSignatureIsCanonical(t *testing.T) {
	left, err := emailBillRoutingRuleSignature(EmailBillRoutingRuleInput{
		Bank: " CMB ", Kind: "Credit", Last4: "1234", Currency: "cny", MailboxID: 3, TargetAccountID: 9,
	})
	require.NoError(t, err)
	right, err := emailBillRoutingRuleSignature(EmailBillRoutingRuleInput{
		Bank: "cmb", Kind: "credit", Last4: "1234", Currency: "CNY", MailboxID: 3, TargetAccountID: 9,
	})
	require.NoError(t, err)

	assert.Equal(t, left, right)
}
