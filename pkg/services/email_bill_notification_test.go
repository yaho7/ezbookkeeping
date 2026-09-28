package services

import (
	"context"
	"errors"
	"github.com/mayswind/ezbookkeeping/pkg/core"
	"github.com/mayswind/ezbookkeeping/pkg/datastore"
	"github.com/mayswind/ezbookkeeping/pkg/emailbill"
	"github.com/mayswind/ezbookkeeping/pkg/mail"
	"github.com/mayswind/ezbookkeeping/pkg/models"
	"github.com/mayswind/ezbookkeeping/pkg/settings"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

type emailBillNotificationTestMailer struct {
	messages []*mail.MailMessage
	err      error
}

func (m *emailBillNotificationTestMailer) SendMail(message *mail.MailMessage) error {
	m.messages = append(m.messages, message)
	return m.err
}

func TestEmailBillNotificationModes(t *testing.T) {
	task := &models.EmailBillSyncTask{Status: "succeeded"}
	n := &settings.EmailBillNotificationConfig{Enabled: true, Mode: "always"}
	assert.True(t, emailBillShouldNotify(task, n))
	n.Mode = "changes_or_errors"
	assert.False(t, emailBillShouldNotify(task, n))
	task.Imported = 1
	assert.True(t, emailBillShouldNotify(task, n))
	n.Mode = "errors_only"
	assert.False(t, emailBillShouldNotify(task, n))
	n.Mode = "changes_only"
	assert.True(t, emailBillShouldNotify(task, n))
	task.Imported = 0
	assert.False(t, emailBillShouldNotify(task, n))
	n.Mode = "errors_only"
	task.Status = "failed"
	assert.True(t, emailBillShouldNotify(task, n))
	task.Status, task.Failed = "partial_success", 1
	assert.True(t, emailBillShouldNotify(task, n))
	n.Mode = "changes_only"
	assert.False(t, emailBillShouldNotify(task, n))
	n.Enabled = false
	assert.False(t, emailBillShouldNotify(task, n))
}

func TestEmailBillNotificationClaimsDeliveryOnceAndFailureDoesNotChangeLedger(t *testing.T) {
	for _, failure := range []bool{false, true} {
		t.Run(map[bool]string{false: "sent", true: "failed"}[failure], func(t *testing.T) {
			t.Chdir("../..")
			store := emailBillDefaultsTestStore(t, new(models.Transaction), new(models.TransactionCategory), new(models.EmailBillSyncTask),
				new(models.EmailBillCandidate), new(models.EmailBillCandidateVariant), new(models.EmailBillClassificationDecision),
				new(models.EmailBillTransactionImportIntent), new(models.EmailBillTransactionImportAttempt))
			c := core.NewCronJobContext("notification-test", 0)
			c.Context = context.WithValue(c.Context, emailBillTaskContextKey{}, int64(50))
			accountID, err := store.Resolve(c, 7, "CNY")
			require.NoError(t, err)
			db := store.UserDataDB(7)
			_, err = db.NewSession(c).Insert(&models.TransactionCategory{CategoryId: 8, Uid: 7, Type: models.CATEGORY_TYPE_EXPENSE, Name: "Food"}, &models.TransactionCategory{CategoryId: 9, ParentCategoryId: 8, Uid: 7, Type: models.CATEGORY_TYPE_EXPENSE, Name: "Dining"})
			require.NoError(t, err)
			_, err = NewEmailBillTransactionImporter().Import(c, 7, 100, 1001, accountID, 9, emailbill.StandardBill{OccurredAt: time.Now().Add(-time.Minute), AmountMinor: 1234, Currency: "CNY", FlowType: "expense"})
			require.NoError(t, err)
			_, err = db.NewSession(c).Insert(&models.EmailBillCandidate{CandidateId: 1001, Uid: 7, SelectedVariantId: 201, CurrentClassificationDecisionId: 301}, &models.EmailBillCandidateVariant{VariantId: 201, CandidateId: 1001, Merchant: "<img src=x onerror=alert(1)>"}, &models.EmailBillClassificationDecision{ClassificationDecisionId: 301, CandidateId: 1001, DecisionType: "fallback"})
			require.NoError(t, err)
			task := &models.EmailBillSyncTask{TaskId: 50, Uid: 7, Status: "succeeded", Imported: 1, NotificationStatus: "pending", CompletedUnixTime: time.Now().Unix(), StartedUnixTime: time.Now().Unix() - 10}
			_, err = db.NewSession(c).Insert(task)
			require.NoError(t, err)
			fake := &emailBillNotificationTestMailer{}
			if failure {
				fake.err = errors.New("SMTP rejected mail-secret and smtp-secret")
			}
			service := &EmailBillNotificationService{ServiceUsingDB: ServiceUsingDB{container: datastore.Container}, newMailer: func(*settings.SMTPConfig) (mail.Mailer, error) { return fake, nil }}
			count, err := service.ImportedCount(c, task)
			require.NoError(t, err)
			assert.Equal(t, int64(1), count)
			config := &settings.EmailBillConfig{MailUser: "alice@qq.com", MailPassword: "mail-secret", Timezone: "Asia/Shanghai", Notification: &settings.EmailBillNotificationConfig{Enabled: true, Mode: "always", UseMailboxCredentials: true, SMTPPassword: "smtp-secret"}}
			err = service.Notify(c, task, config, "https://example.com", "zh-Hans")
			if failure {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.NoError(t, service.Notify(c, task, config, "https://example.com", "zh-Hans"))
			require.Len(t, fake.messages, 1)
			assert.Contains(t, fake.messages[0].Body, "&lt;img")
			assert.NotContains(t, fake.messages[0].Body, "<img src=x")
			stored := &models.EmailBillSyncTask{}
			_, err = db.NewSession(c).ID(50).Get(stored)
			require.NoError(t, err)
			assert.Equal(t, "succeeded", stored.Status)
			assert.Equal(t, map[bool]string{false: "sent", true: "failed"}[failure], stored.NotificationStatus)
			assert.NotContains(t, stored.NotificationError, "mail-secret")
			assert.NotContains(t, stored.NotificationError, "smtp-secret")
			count, err = db.NewSession(c).Count(&models.Transaction{})
			require.NoError(t, err)
			assert.Equal(t, int64(1), count)
			account, err := Accounts.GetAccountByAccountId(c, 7, accountID)
			require.NoError(t, err)
			assert.Equal(t, int64(-1234), account.Balance)
		})
	}
}
