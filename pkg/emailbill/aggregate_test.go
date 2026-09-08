package emailbill

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAggregateMergesEquivalentParserEvidence(t *testing.T) {
	bill := StandardBill{
		ExternalID: "flow-1", OccurredAt: time.Date(2026, 9, 8, 1, 2, 3, 0, time.UTC),
		AmountMinor: -1200, Currency: "CNY", FlowType: "expense", Merchant: "麦当劳",
		AccountHint: AccountHint{Bank: "cmb", Last4: "5460"},
	}
	candidates := AggregateBills(11, "mail-1", []ParserResult{
		{ParserVersionID: 101, Bills: []StandardBill{bill}},
		{ParserVersionID: 102, Bills: []StandardBill{bill}},
	})
	require.Len(t, candidates, 1)
	require.Len(t, candidates[0].Variants, 1)
	assert.Equal(t, []int64{101, 102}, candidates[0].Variants[0].ParserVersionIDs)
	assert.False(t, candidates[0].Conflicted)
}

func TestAggregateDetectsConflictingParserVariants(t *testing.T) {
	base := StandardBill{
		ExternalID: "flow-1", OccurredAt: time.Date(2026, 9, 8, 1, 2, 3, 0, time.UTC),
		AmountMinor: -1200, Currency: "CNY", FlowType: "expense", Merchant: "商户",
		AccountHint: AccountHint{Bank: "cmb", Last4: "5460"},
	}
	changed := base
	changed.AmountMinor = -12000
	candidates := AggregateBills(11, "mail-1", []ParserResult{
		{ParserVersionID: 101, Bills: []StandardBill{base}},
		{ParserVersionID: 102, Bills: []StandardBill{changed}},
	})
	require.Len(t, candidates, 1)
	assert.Len(t, candidates[0].Variants, 2)
	assert.True(t, candidates[0].Conflicted)
}

func TestAggregateKeepsMultipleBillsFromOneMessageSeparate(t *testing.T) {
	first := StandardBill{OccurredAt: time.Now(), AmountMinor: -100, Currency: "CNY", FlowType: "expense"}
	second := first
	second.AmountMinor = -200
	candidates := AggregateBills(11, "mail-1", []ParserResult{{ParserVersionID: 101, Bills: []StandardBill{first, second}}})
	assert.Len(t, candidates, 2)
}
