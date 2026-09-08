package services

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/mayswind/ezbookkeeping/pkg/core"
	"github.com/mayswind/ezbookkeeping/pkg/emailbill"
	"github.com/mayswind/ezbookkeeping/pkg/models"
)

func TestEmailBillImportIntentOnlyClaimsRetryableStates(t *testing.T) {
	assert.True(t, emailBillImportIntentCanClaim("pending"))
	assert.True(t, emailBillImportIntentCanClaim("failed"))
	assert.False(t, emailBillImportIntentCanClaim("processing"))
	assert.False(t, emailBillImportIntentCanClaim("completed"))
}

func TestBuildNativeEmailBillTransactionUsesRoutingAndClassification(t *testing.T) {
	bill := emailbill.StandardBill{
		OccurredAt:  time.Date(2026, 9, 8, 10, 11, 12, 0, time.FixedZone("CST", 8*60*60)),
		AmountMinor: -1234, Currency: "CNY", FlowType: "expense", Merchant: "Coffee", Description: "Latte",
	}

	transaction := buildNativeEmailBillTransaction(core.NewNullContext(), 7, 9, 11, bill, "[ebk-mail:candidate]")

	assert.Equal(t, models.TRANSACTION_DB_TYPE_EXPENSE, transaction.Type)
	assert.Equal(t, int64(9), transaction.AccountId)
	assert.Equal(t, int64(11), transaction.CategoryId)
	assert.Equal(t, int64(1234), transaction.Amount)
	assert.Contains(t, transaction.Comment, "Coffee")
	assert.Contains(t, transaction.Comment, "[ebk-mail:candidate]")
}
