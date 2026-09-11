package services

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mayswind/ezbookkeeping/pkg/core"
	"github.com/mayswind/ezbookkeeping/pkg/emailbill"
)

type fakeEmailBillParserCodeGenerator struct {
	draft EmailBillGeneratedParser
	err   error
}

func (g *fakeEmailBillParserCodeGenerator) Generate(_ core.Context, _ int64, _ EmailBillFetchedMessage) (EmailBillGeneratedParser, error) {
	return g.draft, g.err
}

func TestGenerateParserDraftValidatesGeneratedCodeAgainstSample(t *testing.T) {
	service := NewEmailBillAutomationService(nil, nil)
	service.generator = &fakeEmailBillParserCodeGenerator{draft: EmailBillGeneratedParser{
		Name: "Demo bank", Bank: "demo", Matcher: emailbill.ParserMatcher{Senders: []string{"bank@example.com"}},
		SourceCode: oneBillScript("coffee"),
	}}

	draft, err := service.GenerateParserDraft(core.NewNullContext(), 7, sampleScriptMail())

	require.NoError(t, err)
	assert.Equal(t, "Demo bank", draft.Name)
	assert.Equal(t, "coffee", draft.Preview.Bills[0].Bill.Merchant)
}

func TestGenerateParserDraftPreservesMatcherThatMissesSample(t *testing.T) {
	service := NewEmailBillAutomationService(nil, nil)
	service.generator = &fakeEmailBillParserCodeGenerator{draft: EmailBillGeneratedParser{
		Name: "Unsafe broad draft", Matcher: emailbill.ParserMatcher{Senders: []string{"other@example.com"}},
		SourceCode: oneBillScript("coffee"),
	}}

	draft, err := service.GenerateParserDraft(core.NewNullContext(), 7, sampleScriptMail())
	require.NoError(t, err)
	assert.Equal(t, "not_matched", draft.ValidationStatus)
	assert.NotEmpty(t, draft.SourceCode)
	assert.NotNil(t, draft.Preview.Bills)
}

func TestParseEmailBillGeneratedParserRejectsUnknownFields(t *testing.T) {
	_, err := parseEmailBillGeneratedParser([]byte(`{
        "name":"demo","bank":"demo","matcher":{"senders":["bank@example.com"],"subjectContains":[]},
        "sourceCode":"def parse(mail): return []","unexpected":true
    }`))

	require.Error(t, err)
}

func TestParserGenerationPromptMarksEmailAsUntrusted(t *testing.T) {
	prompt, err := emailBillParserGenerationUserPrompt(sampleScriptMail())

	require.NoError(t, err)
	assert.Contains(t, string(prompt), "UNTRUSTED_EMAIL_JSON")
	assert.Contains(t, string(prompt), `bank@example.com`)
}

func TestGenerateParserDraftKeepsEmptyAndInvalidDraftsEditable(t *testing.T) {
	for _, item := range []struct{ source, status string }{
		{"def parse(mail): return []", "no_bills"},
		{"def parse(mail): return unknown_function()", "sandbox_failed"},
	} {
		t.Run(item.status, func(t *testing.T) {
			service := NewEmailBillAutomationService(nil, nil)
			service.generator = &fakeEmailBillParserCodeGenerator{draft: EmailBillGeneratedParser{
				Name: "Draft", Matcher: emailbill.ParserMatcher{Senders: []string{"bank@example.com"}}, SourceCode: item.source,
			}}
			draft, err := service.GenerateParserDraft(core.NewNullContext(), 7, sampleScriptMail())
			require.NoError(t, err)
			assert.Equal(t, item.status, draft.ValidationStatus)
			assert.Equal(t, item.source, draft.SourceCode)
			if item.status == "no_bills" {
				require.NotNil(t, draft.Preview)
				assert.NotNil(t, draft.Preview.Bills)
			}
		})
	}
}
