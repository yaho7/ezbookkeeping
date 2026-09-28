package services

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mayswind/ezbookkeeping/pkg/core"
	"github.com/mayswind/ezbookkeeping/pkg/emailbill"
	"github.com/mayswind/ezbookkeeping/pkg/models"
)

func TestEmailBillImportWaitsForMailboxSnapshotAndRemainsIdempotent(t *testing.T) {
	s := emailBillDefaultsTestStoreWithConnections(t, 2,
		new(models.Transaction), new(models.TransactionCategory),
		new(models.EmailBillTransactionImportIntent), new(models.EmailBillTransactionImportAttempt))
	c := core.NewNullContext()
	accountID, err := s.Resolve(c, 7, "CNY")
	require.NoError(t, err)
	database := s.UserDataDB(7)
	_, err = database.NewSession(c).Insert(
		&models.TransactionCategory{CategoryId: 8, Uid: 7, Type: models.CATEGORY_TYPE_EXPENSE, Name: "Food"},
		&models.TransactionCategory{CategoryId: 9, Uid: 7, Type: models.CATEGORY_TYPE_EXPENSE, ParentCategoryId: 8, Name: "Dining"})
	require.NoError(t, err)

	// A mailbox snapshot holds a real shared-cache read lock on account.
	// The importer must wait rather than fail while updating its balance.
	EmailBillSync.metadataMu.Lock()
	locked := true
	defer func() {
		if locked {
			EmailBillSync.metadataMu.Unlock()
		}
	}()
	reader := database.NewSession(c)
	defer reader.Close()
	require.NoError(t, reader.Begin())
	has, err := reader.ID(accountID).Get(&models.Account{})
	require.NoError(t, err)
	require.True(t, has)

	bill := emailbill.StandardBill{OccurredAt: time.Now().Add(-time.Minute), AmountMinor: 100, Currency: "CNY", FlowType: "expense"}
	importer := NewEmailBillTransactionImporter()
	type outcome struct {
		id  int64
		err error
	}
	started := make(chan struct{})
	result := make(chan outcome, 1)
	go func() {
		close(started)
		id, err := importer.Import(c, 7, 50, 1001, accountID, 9, bill)
		result <- outcome{id: id, err: err}
	}()
	<-started
	select {
	case got := <-result:
		t.Fatalf("import returned during the account snapshot: id=%d error=%v", got.id, got.err)
	case <-time.After(100 * time.Millisecond):
	}
	require.NoError(t, reader.Commit())
	reader.Close()
	EmailBillSync.metadataMu.Unlock()
	locked = false

	var first outcome
	select {
	case first = <-result:
		require.NoError(t, first.err)
		require.Positive(t, first.id)
	case <-time.After(5 * time.Second):
		t.Fatal("import did not resume after releasing the snapshot")
	}
	again, err := importer.Import(c, 7, 51, 1001, accountID, 9, bill)
	require.NoError(t, err)
	assert.Equal(t, first.id, again)
	count, err := database.NewSession(c).Count(&models.Transaction{})
	require.NoError(t, err)
	assert.Equal(t, int64(1), count)
	count, err = database.NewSession(c).Where("status=?", "completed").Count(&models.EmailBillTransactionImportIntent{})
	require.NoError(t, err)
	assert.Equal(t, int64(1), count)
	account, err := Accounts.GetAccountByAccountId(c, 7, accountID)
	require.NoError(t, err)
	assert.Equal(t, int64(-100), account.Balance)
}

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

func TestBuildNativeEmailBillTransactionTreatsPositiveFlowTypesAsIncome(t *testing.T) {
	for _, flowType := range []string{"income", "refund", "transfer_in"} {
		t.Run(flowType, func(t *testing.T) {
			transaction := buildNativeEmailBillTransaction(core.NewNullContext(), 7, 9, 11, emailbill.StandardBill{
				OccurredAt: time.Unix(1_700_000_000, 0), AmountMinor: 100, FlowType: flowType,
			}, "[ebk-mail:candidate]")

			assert.Equal(t, models.TRANSACTION_DB_TYPE_INCOME, transaction.Type)
		})
	}
}
