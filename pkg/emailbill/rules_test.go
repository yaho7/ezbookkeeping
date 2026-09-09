package emailbill

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveAccountPrefersSpecificHighPriorityRule(t *testing.T) {
	bill := StandardBill{Currency: "CNY", AccountHint: AccountHint{Bank: "cmb", Kind: "credit_card", Last4: "5460"}}
	rules := []AccountRoutingRule{
		{ID: 1, Enabled: true, Priority: 10, Bank: "cmb", TargetAccountID: 100},
		{ID: 2, Enabled: true, Priority: 20, Bank: "cmb", Last4: "5460", TargetAccountID: 200},
	}

	decision, found := ResolveAccount(bill, 9, rules)

	require.True(t, found)
	assert.Equal(t, int64(2), decision.RuleID)
	assert.Equal(t, int64(200), decision.AccountID)
}

func TestResolveAccountLeavesMissingRouteUnresolved(t *testing.T) {
	_, found := ResolveAccount(StandardBill{AccountHint: AccountHint{Bank: "cmb"}}, 9, []AccountRoutingRule{{ID: 1, Enabled: false, Bank: "cmb", TargetAccountID: 100}})
	assert.False(t, found)
}

func TestResolveCategoryOrdersManualBeforeLearnedRules(t *testing.T) {
	bill := StandardBill{Merchant: "麦当劳（人民广场店）", FlowType: "expense", AccountHint: AccountHint{Bank: "cmb"}}
	rules := []ClassificationRule{
		{ID: 1, Enabled: true, Origin: RuleOriginLearnedLLM, MatchType: MatchContains, MerchantPattern: "麦当劳", CategoryID: 100, Priority: 100},
		{ID: 2, Enabled: true, Origin: RuleOriginManual, MatchType: MatchContains, MerchantPattern: "麦当劳", CategoryID: 200, Priority: 1},
	}

	decision, found, err := ResolveCategory(bill, 9, rules)

	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, int64(2), decision.RuleID)
	assert.Equal(t, int64(200), decision.CategoryID)
}

func TestResolveCategorySupportsExactContainsAndRegex(t *testing.T) {
	tests := []struct {
		name     string
		rule     ClassificationRule
		merchant string
	}{
		{name: "exact", rule: ClassificationRule{MatchType: MatchExact, MerchantPattern: "麦当劳"}, merchant: " 麦当劳 "},
		{name: "contains", rule: ClassificationRule{MatchType: MatchContains, MerchantPattern: "麦当劳"}, merchant: "麦当劳上海店"},
		{name: "regex", rule: ClassificationRule{MatchType: MatchRegex, MerchantPattern: `^麦当劳.*店$`}, merchant: "麦当劳上海店"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.rule.ID = 1
			test.rule.Enabled = true
			test.rule.Origin = RuleOriginManual
			test.rule.CategoryID = 7
			decision, found, err := ResolveCategory(StandardBill{Merchant: test.merchant}, 0, []ClassificationRule{test.rule})
			require.NoError(t, err)
			require.True(t, found)
			assert.Equal(t, int64(7), decision.CategoryID)
			assert.Equal(t, int64(1), decision.RuleID)
			assert.Equal(t, test.rule.MatchType, decision.MatchType)
			assert.NotEmpty(t, decision.Reason)
			assert.GreaterOrEqual(t, decision.Confidence, 0.0)
			assert.LessOrEqual(t, decision.Confidence, 1.0)
		})
	}
}
