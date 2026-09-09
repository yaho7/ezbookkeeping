package services

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mayswind/ezbookkeeping/pkg/core"
	"github.com/mayswind/ezbookkeeping/pkg/emailbill"
)

type memoryEmailBillPipelineRepository struct {
	mu         sync.Mutex
	messages   map[string]int64
	nextID     int64
	runs       []EmailBillPersistedRun
	parserRuns []EmailBillPersistedParserRun
	candidates []emailbill.AggregatedCandidate
	lastInput  EmailBillMessageInput
}

func newMemoryEmailBillPipelineRepository() *memoryEmailBillPipelineRepository {
	return &memoryEmailBillPipelineRepository{messages: make(map[string]int64), nextID: 100}
}

func (r *memoryEmailBillPipelineRepository) SaveMessageAndStartRun(_ core.Context, input EmailBillMessageInput) (int64, int64, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lastInput = input
	if messageID, exists := r.messages[input.Fingerprint]; exists {
		return messageID, 0, true, nil
	}
	r.nextID++
	messageID := r.nextID
	r.nextID++
	runID := r.nextID
	r.messages[input.Fingerprint] = messageID
	r.runs = append(r.runs, EmailBillPersistedRun{RunID: runID, MessageID: messageID, Status: "running"})
	return messageID, runID, false, nil
}

func TestEmailBillPipelineOnlyPersistsRawBodyWhenExplicitlyRequested(t *testing.T) {
	repository := newMemoryEmailBillPipelineRepository()
	pipeline := NewEmailBillPipeline(repository, emailbill.NewScriptParser(emailbill.ScriptLimits{}))
	mail := authenticatedScriptMail()

	_, err := pipeline.ProcessMessage(core.NewNullContext(), 7, 3, mail, nil)
	require.NoError(t, err)
	assert.Empty(t, repository.lastInput.BodyContent)

	repository = newMemoryEmailBillPipelineRepository()
	pipeline = NewEmailBillPipeline(repository, emailbill.NewScriptParser(emailbill.ScriptLimits{}))
	mail.RetainBody = true
	_, err = pipeline.ProcessMessage(core.NewNullContext(), 7, 3, mail, nil)
	require.NoError(t, err)
	assert.Equal(t, mail.Text, repository.lastInput.BodyContent)
}

func (r *memoryEmailBillPipelineRepository) SaveParserRun(_ core.Context, run EmailBillPersistedParserRun) ([]EmailBillSavedOutput, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextID++
	r.parserRuns = append(r.parserRuns, run)
	outputs := make([]EmailBillSavedOutput, len(run.Bills))
	for index, bill := range run.Bills {
		r.nextID++
		outputs[index] = EmailBillSavedOutput{OutputID: r.nextID, Bill: bill}
	}
	return outputs, nil
}

func (r *memoryEmailBillPipelineRepository) SaveCandidates(_ core.Context, _, _ int64, _ string, candidates []emailbill.AggregatedCandidate, _ map[int64][]EmailBillSavedOutput) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.candidates = append(r.candidates, candidates...)
	return nil
}

func (r *memoryEmailBillPipelineRepository) FinishRun(_ core.Context, _ int64, runID int64, status string, _ string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for index := range r.runs {
		if r.runs[index].RunID == runID {
			r.runs[index].Status = status
		}
	}
	return nil
}

func TestEmailBillPipelineDoesNotRepeatAnAlreadyPersistedMessage(t *testing.T) {
	repository := newMemoryEmailBillPipelineRepository()
	pipeline := NewEmailBillPipeline(repository, emailbill.NewScriptParser(emailbill.ScriptLimits{}))
	mail := authenticatedScriptMail()
	rules := []emailbill.RunnableParserRule{{VersionID: 11, Matcher: emailbill.ParserMatcher{}, Source: oneBillScript("coffee")}}

	first, err := pipeline.ProcessMessage(core.NewNullContext(), 7, 3, mail, rules)
	require.NoError(t, err)
	second, err := pipeline.ProcessMessage(core.NewNullContext(), 7, 3, mail, rules)
	require.NoError(t, err)

	assert.False(t, first.Duplicate)
	assert.True(t, second.Duplicate)
	assert.Len(t, repository.runs, 1)
	assert.Len(t, repository.parserRuns, 1)
	assert.Len(t, repository.candidates, 1)
}

func TestEmailBillPipelineRunsMatchingRulesIndependently(t *testing.T) {
	repository := newMemoryEmailBillPipelineRepository()
	pipeline := NewEmailBillPipeline(repository, emailbill.NewScriptParser(emailbill.ScriptLimits{}))
	rules := []emailbill.RunnableParserRule{
		{VersionID: 11, Matcher: emailbill.ParserMatcher{}, Source: oneBillScript("coffee")},
		{VersionID: 12, Matcher: emailbill.ParserMatcher{}, Source: "def parse(mail):\n  fail(\"broken parser\")"},
		{VersionID: 13, Matcher: emailbill.ParserMatcher{SubjectContains: []string{"unrelated"}}, Source: oneBillScript("ignored")},
	}

	result, err := pipeline.ProcessMessage(core.NewNullContext(), 7, 3, authenticatedScriptMail(), rules)
	require.NoError(t, err)

	assert.Equal(t, "partial_success", result.Status)
	require.Len(t, repository.parserRuns, 3)
	assert.Equal(t, "succeeded", repository.parserRuns[0].Status)
	assert.Equal(t, "failed", repository.parserRuns[1].Status)
	assert.Equal(t, "not_matched", repository.parserRuns[2].Status)
	assert.Len(t, repository.candidates, 1)
}

func TestEmailBillPipelineRejectsUnauthenticatedMailBeforeScripts(t *testing.T) {
	repository := newMemoryEmailBillPipelineRepository()
	pipeline := NewEmailBillPipeline(repository, emailbill.NewScriptParser(emailbill.ScriptLimits{}))
	mail := authenticatedScriptMail()
	mail.Authenticated = false

	result, err := pipeline.ProcessMessage(core.NewNullContext(), 7, 3, mail, []emailbill.RunnableParserRule{{
		VersionID: 11,
		Matcher:   emailbill.ParserMatcher{},
		Source:    "def parse(mail):\n  fail(\"must not execute\")",
	}})
	require.NoError(t, err)

	assert.Equal(t, "rejected", result.Status)
	assert.Empty(t, repository.parserRuns)
	assert.Empty(t, repository.candidates)
}

func TestRetainedEmailBillBodyRequiresExplicitOptIn(t *testing.T) {
	message := authenticatedScriptMail()
	assert.Empty(t, retainedEmailBillBody(message))

	message.RetainBody = true
	assert.Equal(t, message.Text, retainedEmailBillBody(message))
}

func TestEmailBillOutputEvidenceMatchesExactVariantOnly(t *testing.T) {
	bill := emailbill.StandardBill{
		ExternalID: "order-1", OccurredAt: time.Date(2026, 9, 8, 10, 0, 0, 0, time.UTC),
		AmountMinor: 1200, Currency: "CNY", FlowType: "expense", Merchant: "coffee",
	}
	other := bill
	other.AmountMinor = 1500
	identity, _ := emailbill.CandidateIdentity(emailbill.CandidateIdentityInput{UserID: 7, Bank: "demo", ExternalID: "order-1"})
	fingerprint, _ := emailbill.BillFingerprint(emailbill.BillFingerprintInput{
		IdentityKey: identity, TransactionTime: bill.OccurredAt, AmountMinor: bill.AmountMinor,
		Currency: bill.Currency, Direction: bill.FlowType, Merchant: bill.Merchant,
	})

	matched := matchingEmailBillOutputIDs(identity, fingerprint, []EmailBillSavedOutput{
		{OutputID: 21, Bill: other}, {OutputID: 22, Bill: bill},
	})

	assert.Equal(t, []int64{22}, matched)
}

func authenticatedScriptMail() EmailBillFetchedMessage {
	return EmailBillFetchedMessage{
		RemoteMessageID: "<bill-1@example.com>",
		Sender:          "bank@example.com",
		Subject:         "card bill",
		ReceivedAt:      time.Date(2026, 9, 8, 10, 0, 0, 0, time.UTC),
		Text:            "merchant: coffee",
		Authenticated:   true,
		Headers:         map[string]string{"authentication-results": "dkim=pass"},
	}
}

func oneBillScript(merchant string) string {
	if merchant == "" {
		panic("merchant required")
	}
	return "def parse(mail):\n  return [{\"external_id\": \"order-1\", \"occurred_at\": \"2026-09-08T10:00:00Z\", \"amount\": \"12.00\", \"currency\": \"CNY\", \"flow_type\": \"expense\", \"merchant\": \"" + merchant + "\", \"description\": \"latte\", \"account_hint\": {\"bank\": \"demo\", \"kind\": \"credit\", \"last4\": \"1234\"}}]"
}
