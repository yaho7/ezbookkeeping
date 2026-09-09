package api

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mayswind/ezbookkeeping/pkg/emailbill"
	"github.com/mayswind/ezbookkeeping/pkg/models"
	"github.com/mayswind/ezbookkeeping/pkg/services"
)

func TestEmailBillParserRuleResponseUsesStringIDsAndVersion(t *testing.T) {
	response := emailBillParserRuleResponse(&services.EmailBillParserRuleInfo{
		Rule:    &models.EmailBillParserRule{ParserRuleId: 9007199254740993, Name: "bank", Enabled: true, CurrentVersionId: 8},
		Version: &models.EmailBillParserRuleVersion{ParserRuleVersionId: 8, Version: 3, MatcherJson: `{}`, SourceCode: "def parse(mail): return []", CreatedBy: "preset:cmb_credit"},
	})
	raw, err := json.Marshal(response)
	require.NoError(t, err)

	assert.Contains(t, string(raw), `"id":"9007199254740993"`)
	assert.Contains(t, string(raw), `"version":3`)
	assert.Contains(t, string(raw), `"createdBy":"preset:cmb_credit"`)
	assert.NotContains(t, string(raw), `currentVersionId`)
}

func TestEmailBillParserTestRequestParsesRFC3339MailTime(t *testing.T) {
	request, err := emailBillParserTestServiceRequest(7, emailBillParserTestRequest{
		Matcher: emailbill.ParserMatcher{}, SourceCode: "def parse(mail): return []",
		Mail: emailBillTestMailRequest{MessageID: "m1", Sender: "bank@example.com", ReceivedAt: "2026-09-08T10:00:00+08:00"},
	})
	require.NoError(t, err)

	assert.Equal(t, int64(7), request.UID)
	assert.Equal(t, 10, request.Mail.ReceivedAt.Hour())
	_, offset := request.Mail.ReceivedAt.Zone()
	assert.Equal(t, 8*60*60, offset)
}
