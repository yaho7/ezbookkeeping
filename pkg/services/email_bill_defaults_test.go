package services

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mayswind/ezbookkeeping/pkg/core"
	"github.com/mayswind/ezbookkeeping/pkg/datastore"
	"github.com/mayswind/ezbookkeeping/pkg/emailbill"
	"github.com/mayswind/ezbookkeeping/pkg/models"
	"github.com/mayswind/ezbookkeeping/pkg/settings"
	"github.com/mayswind/ezbookkeeping/pkg/uuid"
)

// These database tests run serially and restore the shared service containers.
func emailBillDefaultsTestStore(t *testing.T, additionalModels ...any) *EmailBillDefaultAccountService {
	t.Helper()
	previousDB, previousUUID := *datastore.Container, *uuid.Container
	t.Cleanup(func() { *datastore.Container, *uuid.Container = previousDB, previousUUID })
	config := &settings.Config{
		DatabaseConfig: &settings.DatabaseConfig{DatabaseType: settings.Sqlite3DbType,
			DatabasePath: filepath.Join(t.TempDir(), "email-defaults.db"), MaxOpenConnection: 1, MaxIdleConnection: 1},
		UuidGeneratorType: settings.InternalUuidGeneratorType,
	}
	require.NoError(t, datastore.InitializeDataStore(config))
	require.NoError(t, uuid.InitializeUuidGenerator(config))
	beans := []any{new(models.User), new(models.Account), new(models.EmailBillDefaultAccount)}
	require.NoError(t, datastore.Container.UserDataStore.SyncStructs(append(beans, additionalModels...)...))
	s := &EmailBillDefaultAccountService{
		ServiceUsingDB:   ServiceUsingDB{container: datastore.Container},
		ServiceUsingUuid: ServiceUsingUuid{container: uuid.Container},
	}
	_, err := s.UserDB().NewSession(core.NewNullContext()).Insert(&models.User{Uid: 7, Username: "alice", Language: "zh-Hans", DefaultCurrency: "CNY"})
	require.NoError(t, err)
	return s
}

func TestEmailBillDefaultAccountPersistsAcrossRenamesAndRestarts(t *testing.T) {
	s := emailBillDefaultsTestStore(t)
	c := core.NewNullContext()
	first, err := s.Resolve(c, 7, "CNY")
	require.NoError(t, err)
	account, err := Accounts.GetAccountByAccountId(c, 7, first)
	require.NoError(t, err)
	assert.Equal(t, "默认记账账户 (CNY)", account.Name)
	assert.Zero(t, account.Balance)
	user, err := Users.GetUserById(c, 7)
	require.NoError(t, err)
	assert.Equal(t, first, user.DefaultAccountId)
	_, err = s.UserDataDB(7).NewSession(c).ID(first).Cols("name").Update(&models.Account{Name: "My renamed account"})
	require.NoError(t, err)
	_, err = s.UserDB().NewSession(c).ID(7).Cols("default_account_id").Update(&models.User{DefaultAccountId: 0})
	require.NoError(t, err)
	restarted := &EmailBillDefaultAccountService{ServiceUsingDB: s.ServiceUsingDB, ServiceUsingUuid: s.ServiceUsingUuid}
	second, err := restarted.Resolve(c, 7, "cny")
	require.NoError(t, err)
	assert.Equal(t, first, second)
	accounts, err := Accounts.GetAllAccountsByUid(c, 7)
	require.NoError(t, err)
	assert.Len(t, accounts, 1)
}

func TestEmailBillDefaultsRespectProfileAndSeparateCurrencies(t *testing.T) {
	s := emailBillDefaultsTestStore(t)
	c := core.NewNullContext()
	_, err := s.UserDataDB(7).NewSession(c).Insert(&models.Account{AccountId: 101, Uid: 7, Name: "My bank", Type: models.ACCOUNT_TYPE_SINGLE_ACCOUNT, Currency: "CNY"})
	require.NoError(t, err)
	require.NoError(t, Users.SetDefaultAccountIfUnchanged(c, 7, 0, 101))
	id, err := s.Resolve(c, 7, "CNY")
	require.NoError(t, err)
	assert.Equal(t, int64(101), id)
	usdID, err := s.Resolve(c, 7, "USD")
	require.NoError(t, err)
	assert.NotEqual(t, id, usdID)
	user, err := Users.GetUserById(c, 7)
	require.NoError(t, err)
	assert.Equal(t, id, user.DefaultAccountId)
	again, err := s.Resolve(c, 7, "USD")
	require.NoError(t, err)
	assert.Equal(t, usdID, again)
	// A stale read must not change the user's explicit default.
	require.NoError(t, Users.SetDefaultAccountIfUnchanged(c, 7, 0, usdID))
	user, err = Users.GetUserById(c, 7)
	require.NoError(t, err)
	assert.Equal(t, id, user.DefaultAccountId)
}

func TestEmailBillDefaultReplacesHiddenMappedAccount(t *testing.T) {
	s := emailBillDefaultsTestStore(t)
	c := core.NewNullContext()
	first, err := s.Resolve(c, 7, "CNY")
	require.NoError(t, err)
	_, err = s.UserDataDB(7).NewSession(c).ID(first).Cols("hidden").Update(&models.Account{Hidden: true})
	require.NoError(t, err)
	second, err := s.Resolve(c, 7, "CNY")
	require.NoError(t, err)
	assert.NotEqual(t, first, second)
	third, err := s.Resolve(c, 7, "CNY")
	require.NoError(t, err)
	assert.Equal(t, second, third)
}

func TestEmailBillDefaultRejectsUnusableAccounts(t *testing.T) {
	parent := &models.Account{AccountId: 1, Uid: 7, Type: models.ACCOUNT_TYPE_MULTI_SUB_ACCOUNTS}
	child := &models.Account{AccountId: 2, ParentAccountId: 1, Uid: 7, Type: models.ACCOUNT_TYPE_SINGLE_ACCOUNT, Currency: "CNY"}
	accounts := []*models.Account{parent, child}
	assert.Same(t, child, usableEmailBillAccount(accounts, 2, 7, "CNY"))
	assert.Nil(t, usableEmailBillAccount(accounts, 1, 7, ""))
	assert.Nil(t, usableEmailBillAccount(accounts, 2, 8, "CNY"))
	assert.Nil(t, usableEmailBillAccount(accounts, 2, 7, "USD"))
	parent.Hidden = true
	assert.Nil(t, usableEmailBillAccount(accounts, 2, 7, "CNY"))
	parent.Hidden = false
	child.Deleted = true
	assert.Nil(t, usableEmailBillAccount(accounts, 2, 7, "CNY"))
}

type fakeEmailBillDefaultAccountResolver struct {
	calls int
	id    int64
	err   error
}

func (r *fakeEmailBillDefaultAccountResolver) Resolve(core.Context, int64, string) (int64, error) {
	r.calls++
	return r.id, r.err
}

func TestEmailBillRoutingUsesDefaultsOnlyWithoutMatchingRule(t *testing.T) {
	defaults := &fakeEmailBillDefaultAccountResolver{id: 101}
	finalizer := &EmailBillFinalizer{defaultAccounts: defaults}
	bill := emailbill.StandardBill{Currency: "CNY"}
	rules := []emailbill.AccountRoutingRule{{ID: 11, Enabled: true, Currency: "CNY", TargetAccountID: 102}}
	decision, found, err := finalizer.resolveAccount(core.NewNullContext(), 7, 7, bill, rules)
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, int64(102), decision.AccountID)
	assert.Zero(t, defaults.calls)
	decision, found, err = finalizer.resolveAccount(core.NewNullContext(), 7, 7, bill, nil)
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, int64(101), decision.AccountID)
	assert.Zero(t, decision.RuleID)
	defaults.err = errors.New("database unavailable")
	decision, found, err = finalizer.resolveAccount(core.NewNullContext(), 7, 7, bill, nil)
	require.Error(t, err)
	assert.False(t, found)
	assert.Contains(t, decision.Reason, "database unavailable")
}

func TestEmailBillClassifiedCandidateRetainsAccountFailure(t *testing.T) {
	assert.Equal(t, "awaiting_account", emailBillClassifiedCandidateStatus(false, 12))
	assert.Equal(t, "awaiting_account", emailBillClassifiedCandidateStatus(false, 0))
	assert.Equal(t, "awaiting_classification", emailBillClassifiedCandidateStatus(true, 0))
	assert.Equal(t, "ready", emailBillClassifiedCandidateStatus(true, 12))
}

func TestEmailBillFinalizerCallsAIWhenDefaultAccountPreparationFails(t *testing.T) {
	s := emailBillDefaultsTestStore(t,
		new(models.EmailBillAccountRoutingRule), new(models.EmailBillAccountRoutingRuleVersion),
		new(models.EmailBillClassificationRule), new(models.EmailBillClassificationRuleVersion),
		new(models.EmailBillCandidate), new(models.EmailBillCandidateVariant), new(models.TransactionCategory),
		new(models.EmailBillAccountRoutingDecision), new(models.EmailBillClassificationDecision),
		new(models.EmailBillLLMClassificationRun))
	c := core.NewNullContext()
	candidate := &models.EmailBillCandidate{CandidateId: 30, Uid: 7, MessageId: 20, SelectedVariantId: 40, Status: "awaiting_account"}
	_, err := s.UserDataDB(7).NewSession(c).Insert(candidate,
		&models.EmailBillCandidateVariant{VariantId: 40, CandidateId: 30, Currency: "CNY", Direction: "expense", Amount: -100},
		&models.TransactionCategory{CategoryId: 8, Uid: 7, Type: models.CATEGORY_TYPE_EXPENSE, Name: "Food"},
		&models.TransactionCategory{CategoryId: 9, Uid: 7, Type: models.CATEGORY_TYPE_EXPENSE, ParentCategoryId: 8, Name: "Dining"})
	require.NoError(t, err)
	client := &fakeEmailBillLLMClient{result: EmailBillLLMResult{CategoryID: 9, Confidence: 0.95}}
	finalizer := &EmailBillFinalizer{
		db: datastore.Container, uuids: uuid.Container, automation: NewEmailBillAutomationService(datastore.Container, uuid.Container),
		classifier:      NewEmailBillClassifier(client, 0.8),
		defaultAccounts: &fakeEmailBillDefaultAccountResolver{err: errors.New("cannot create account")},
	}
	require.ErrorContains(t, finalizer.finalizeCandidate(c, 7, 7, 50, candidate), "cannot create account")
	assert.Equal(t, 1, client.calls)
	stored := &models.EmailBillCandidate{}
	has, err := s.UserDataDB(7).NewSession(c).ID(30).Get(stored)
	require.NoError(t, err)
	require.True(t, has)
	assert.Equal(t, "awaiting_account", stored.Status)
	classification := &models.EmailBillClassificationDecision{}
	has, err = s.UserDataDB(7).NewSession(c).ID(stored.CurrentClassificationDecisionId).Get(classification)
	require.NoError(t, err)
	require.True(t, has)
	assert.Equal(t, int64(9), classification.CategoryId)
	assert.Equal(t, "llm_existing", classification.DecisionType)
	// The next account retry reuses the persisted classification and LLM run.
	require.ErrorContains(t, finalizer.finalizeCandidate(c, 7, 7, 50, stored), "cannot create account")
	assert.Equal(t, 1, client.calls)
	runs, err := s.UserDataDB(7).NewSession(c).Count(&models.EmailBillLLMClassificationRun{})
	require.NoError(t, err)
	assert.Equal(t, int64(1), runs)
	_, _, reused, err := finalizer.classificationForAccountRetry(c, 7, stored, nil)
	require.NoError(t, err)
	assert.False(t, reused, "a deleted category must not be reused")
}
