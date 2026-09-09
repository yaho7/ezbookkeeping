package services

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mayswind/ezbookkeeping/pkg/core"
	"github.com/mayswind/ezbookkeeping/pkg/emailbill"
)

type fakeEmailBillLLMClient struct {
	calls      int
	categories []EmailBillCategoryOption
	result     EmailBillLLMResult
	err        error
}

func (c *fakeEmailBillLLMClient) Classify(_ core.Context, _ int64, _ emailbill.StandardBill, categories []EmailBillCategoryOption) (EmailBillLLMResult, error) {
	c.calls++
	c.categories = append([]EmailBillCategoryOption(nil), categories...)
	return c.result, c.err
}

func TestEmailBillClassifierSkipsLLMWhenRuleMatches(t *testing.T) {
	client := &fakeEmailBillLLMClient{}
	classifier := NewEmailBillClassifier(client, 0.8)
	bill := emailbill.StandardBill{Merchant: "Coffee Shop", FlowType: "expense"}
	rules := []emailbill.ClassificationRule{{
		ID: 3, Enabled: true, Origin: emailbill.RuleOriginManual, MatchType: emailbill.MatchExact,
		MerchantPattern: "Coffee Shop", CategoryID: 9,
	}}

	decision, err := classifier.Classify(core.NewNullContext(), 7, bill, 2, rules, []EmailBillCategoryOption{{ID: 9, Name: "Food"}})
	require.NoError(t, err)

	assert.Equal(t, "rule", decision.Source)
	assert.Equal(t, int64(9), decision.CategoryID)
	assert.Zero(t, client.calls)
}

func TestEmailBillClassifierPassesExistingCategoriesToLLM(t *testing.T) {
	client := &fakeEmailBillLLMClient{result: EmailBillLLMResult{CategoryID: 12, Confidence: 0.91, Reason: "meal"}}
	classifier := NewEmailBillClassifier(client, 0.8)
	categories := []EmailBillCategoryOption{{ID: 12, Name: "Dining"}, {ID: 13, Name: "Transport"}}

	decision, err := classifier.Classify(core.NewNullContext(), 7, emailbill.StandardBill{Merchant: "Cafe", FlowType: "expense"}, 2, nil, categories)
	require.NoError(t, err)

	assert.Equal(t, "llm_existing", decision.Source)
	assert.Equal(t, int64(12), decision.CategoryID)
	assert.Equal(t, categories, client.categories)
	assert.Equal(t, 1, client.calls)
}

func TestEmailBillClassifierRejectsUnknownLLMCategory(t *testing.T) {
	client := &fakeEmailBillLLMClient{result: EmailBillLLMResult{CategoryID: 999, Confidence: 0.99}}
	classifier := NewEmailBillClassifier(client, 0.8)

	decision, err := classifier.Classify(core.NewNullContext(), 7, emailbill.StandardBill{Merchant: "Cafe"}, 2, nil, []EmailBillCategoryOption{{ID: 12, Name: "Dining"}})
	require.NoError(t, err)

	assert.Equal(t, "awaiting_confirmation", decision.Source)
	assert.Zero(t, decision.CategoryID)
}

func TestEmailBillClassifierKeepsLowConfidenceNewCategoryAsProposal(t *testing.T) {
	client := &fakeEmailBillLLMClient{result: EmailBillLLMResult{NewCategoryName: "Specialty", Confidence: 0.6}}
	classifier := NewEmailBillClassifier(client, 0.8)

	decision, err := classifier.Classify(core.NewNullContext(), 7, emailbill.StandardBill{Merchant: "Rare Shop"}, 2, nil, nil)
	require.NoError(t, err)

	assert.Equal(t, "awaiting_confirmation", decision.Source)
	assert.Equal(t, "Specialty", decision.ProposedCategoryName)
}
