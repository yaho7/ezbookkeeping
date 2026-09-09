package emailbill

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunParserRulesRunsEveryMatchingRuleIndependently(t *testing.T) {
	rules := []RunnableParserRule{
		{VersionID: 1, Matcher: ParserMatcher{Senders: []string{"bank@example.com"}}, Source: `def parse(mail): fail("broken")`},
		{VersionID: 2, Matcher: ParserMatcher{SubjectContains: []string{"账单"}}, Source: `def parse(mail): return [{"occurred_at":"2026-09-08T00:00:00Z","amount":"-1.00","currency":"CNY","flow_type":"expense"}]`},
		{VersionID: 3, Matcher: ParserMatcher{Senders: []string{"other@example.com"}}, Source: `def parse(mail): return []`},
	}

	results := RunParserRules(NewScriptParser(ScriptLimits{}), ScriptMail{Sender: "bank@example.com", Subject: "账单通知"}, rules)

	require.Len(t, results, 3)
	assert.True(t, results[0].Matched)
	assert.Error(t, results[0].Err)
	assert.True(t, results[1].Matched)
	assert.NoError(t, results[1].Err)
	assert.Len(t, results[1].Bills, 1)
	assert.False(t, results[2].Matched)
}
