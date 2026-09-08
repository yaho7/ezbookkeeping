package emailbill

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScriptParserReturnsStandardBills(t *testing.T) {
	parser := NewScriptParser(ScriptLimits{MaxExecutionSteps: 10000, MaxOutputs: 4})
	source := `
def parse(mail):
    return [{
        "external_id": "tx-1",
        "occurred_at": "2026-09-08T08:30:00+08:00",
        "amount": "-12.34",
        "currency": "CNY",
        "flow_type": "expense",
        "merchant": "麦当劳",
        "description": mail["subject"],
        "account_hint": {"bank": "cmb", "kind": "credit_card", "last4": "5460"},
    }]
`

	bills, stats, err := parser.Parse(source, ScriptMail{Subject: "消费通知"})

	require.NoError(t, err)
	require.Len(t, bills, 1)
	assert.Equal(t, int64(-1234), bills[0].AmountMinor)
	assert.Equal(t, "麦当劳", bills[0].Merchant)
	assert.Equal(t, "5460", bills[0].AccountHint.Last4)
	assert.Greater(t, stats.ExecutionSteps, uint64(0))
}

func TestScriptParserRejectsModuleLoading(t *testing.T) {
	parser := NewScriptParser(ScriptLimits{MaxExecutionSteps: 10000, MaxOutputs: 4})

	source := `load("unsafe.star", "value")
def parse(mail):
    return []
`
	_, _, err := parser.Parse(source, ScriptMail{})

	assert.ErrorContains(t, err, "load")
}

func TestScriptParserStopsAtExecutionLimit(t *testing.T) {
	parser := NewScriptParser(ScriptLimits{MaxExecutionSteps: 100, MaxOutputs: 4})

	source := `def parse(mail):
    total = 0
    for i in range(1000000):
        total += i
    return []
`
	_, _, err := parser.Parse(source, ScriptMail{})

	assert.Error(t, err)
}

func TestScriptParserRejectsTooManyOutputs(t *testing.T) {
	parser := NewScriptParser(ScriptLimits{MaxExecutionSteps: 10000, MaxOutputs: 1})
	source := `def parse(mail):
    item = {"occurred_at": "2026-09-08T08:30:00Z", "amount": "1.00", "currency": "CNY", "flow_type": "income"}
    return [item, item]
`

	_, _, err := parser.Parse(source, ScriptMail{})

	assert.ErrorContains(t, err, "at most 1")
}

func TestParserMatcherMatchesSenderAndSubject(t *testing.T) {
	matcher := ParserMatcher{Senders: []string{"notice@example.com"}, SubjectContains: []string{"账单"}}

	assert.True(t, matcher.Matches(ScriptMail{Sender: "NOTICE@example.com", Subject: "本月账单通知"}))
	assert.False(t, matcher.Matches(ScriptMail{Sender: "other@example.com", Subject: "本月账单通知"}))
	assert.False(t, matcher.Matches(ScriptMail{Sender: "notice@example.com", Subject: "验证码"}))
}

func TestScriptParserRejectsInvalidBill(t *testing.T) {
	parser := NewScriptParser(ScriptLimits{MaxExecutionSteps: 10000, MaxOutputs: 4})

	_, _, err := parser.Parse(`def parse(mail): return [{"amount": "oops"}]`, ScriptMail{ReceivedAt: time.Now()})

	assert.ErrorContains(t, err, "amount")
}
