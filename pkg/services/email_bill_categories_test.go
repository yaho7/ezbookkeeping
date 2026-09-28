package services

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mayswind/ezbookkeeping/pkg/core"
	"github.com/mayswind/ezbookkeeping/pkg/datastore"
	"github.com/mayswind/ezbookkeeping/pkg/models"
	"github.com/mayswind/ezbookkeeping/pkg/uuid"
)

func TestEmailBillCreatedCategoryIsAcceptedByNativeTransactions(t *testing.T) {
	s := emailBillDefaultsTestStore(t, new(models.TransactionCategory), new(models.EmailBillCategoryCreationClaim))
	c := core.NewNullContext()
	finalizer := &EmailBillFinalizer{db: datastore.Container, uuids: uuid.Container}
	for _, categoryType := range []models.TransactionCategoryType{models.CATEGORY_TYPE_EXPENSE, models.CATEGORY_TYPE_INCOME} {
		id, err := finalizer.createClaimedCategory(c, 7, categoryType, "未分类")
		require.NoError(t, err)
		transactionType := models.TRANSACTION_DB_TYPE_EXPENSE
		if categoryType == models.CATEGORY_TYPE_INCOME {
			transactionType = models.TRANSACTION_DB_TYPE_INCOME
		}
		session := s.UserDataDB(7).NewSession(c)
		require.NoError(t, Transactions.isCategoryValid(session, &models.Transaction{Uid: 7, Type: transactionType, CategoryId: id}))
		session.Close()
		again, err := finalizer.createClaimedCategory(c, 7, categoryType, "未分类")
		require.NoError(t, err)
		assert.Equal(t, id, again)
	}
}

func TestEmailBillCategoryRepairsLegacyPrimaryClaimAndHiddenCategory(t *testing.T) {
	s := emailBillDefaultsTestStore(t, new(models.TransactionCategory), new(models.EmailBillCategoryCreationClaim))
	c := core.NewNullContext()
	_, err := s.UserDataDB(7).NewSession(c).Insert(
		&models.TransactionCategory{CategoryId: 9, Uid: 7, Type: models.CATEGORY_TYPE_EXPENSE, Name: "Dining"},
		&models.EmailBillCategoryCreationClaim{CategoryClaimId: 11, Uid: 7, CategoryType: models.CATEGORY_TYPE_EXPENSE, NormalizedName: "dining", CategoryId: 9})
	require.NoError(t, err)
	finalizer := &EmailBillFinalizer{db: datastore.Container, uuids: uuid.Container}
	id, err := finalizer.createClaimedCategory(c, 7, models.CATEGORY_TYPE_EXPENSE, "Dining")
	require.NoError(t, err)
	assert.NotEqual(t, int64(9), id)
	_, err = s.UserDataDB(7).NewSession(c).ID(id).Cols("hidden").Update(&models.TransactionCategory{Hidden: true})
	require.NoError(t, err)
	replacement, err := finalizer.createClaimedCategory(c, 7, models.CATEGORY_TYPE_EXPENSE, "Dining")
	require.NoError(t, err)
	assert.NotEqual(t, id, replacement)
	session := s.UserDataDB(7).NewSession(c)
	defer session.Close()
	require.NoError(t, Transactions.isCategoryValid(session, &models.Transaction{Uid: 7, Type: models.TRANSACTION_DB_TYPE_EXPENSE, CategoryId: replacement}))
}

func TestEmailBillCategorySnapshotExcludesPrimaryAndHiddenParents(t *testing.T) {
	categories := []*models.TransactionCategory{
		{CategoryId: 1, Name: "Food"}, {CategoryId: 2, ParentCategoryId: 1, Name: "Dining"},
		{CategoryId: 3, Hidden: true}, {CategoryId: 4, ParentCategoryId: 3},
		{CategoryId: 5, ParentCategoryId: 1, Hidden: true}, {CategoryId: 6, ParentCategoryId: 99},
	}
	assert.Equal(t, []EmailBillCategoryOption{{ID: 2, Name: "Food / Dining"}}, emailBillUsableCategoryOptions(categories))
}
