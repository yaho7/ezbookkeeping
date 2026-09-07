package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mayswind/ezbookkeeping/pkg/core"
	"github.com/mayswind/ezbookkeeping/pkg/emailbill"
	"github.com/mayswind/ezbookkeeping/pkg/models"
	"github.com/mayswind/ezbookkeeping/pkg/settings"
)

type fakeEmailBillConfigProvider struct {
	config *settings.Config
}

func (p *fakeEmailBillConfigProvider) GetCurrentConfig() *settings.Config { return p.config }

type fakeEmailBillUserService struct {
	user *models.User
}

func (s *fakeEmailBillUserService) GetUserByUsername(core.Context, string) (*models.User, error) {
	return s.user, nil
}

type fakeEmailBillTransactionService struct {
	created         []*models.Transaction
	markers         map[string]bool
	createErr       error
	markBeforeError bool
}

func (s *fakeEmailBillTransactionService) HasEmailBillMarker(_ core.Context, _ int64, marker string) (bool, error) {
	return s.markers[marker], nil
}

func (s *fakeEmailBillTransactionService) CreateTransaction(_ core.Context, transaction *models.Transaction, _ []int64, _ []int64) error {
	if s.createErr != nil {
		if s.markBeforeError {
			s.markers[emailBillMarker(transaction.Comment)] = true
		}
		return s.createErr
	}
	s.created = append(s.created, transaction)
	s.markers[emailBillMarker(transaction.Comment)] = true
	return nil
}

func TestEmailBillImporterTreatsConcurrentDuplicateAsImported(t *testing.T) {
	config := &settings.Config{EmailBillConfig: &settings.EmailBillConfig{
		Enabled:            true,
		TargetUser:         "alice",
		CMBCreditAccountID: 101,
		CMBDebitAccountID:  102,
		ExpenseCategoryID:  201,
		IncomeCategoryID:   202,
		Timezone:           "Asia/Shanghai",
	}}
	transactions := &fakeEmailBillTransactionService{
		markers:         make(map[string]bool),
		createErr:       errors.New("duplicate transaction time"),
		markBeforeError: true,
	}
	mailbox := &fakeEmailBillMailbox{messages: []emailbill.Message{{
		Fingerprint:   "<bill-race@example.com>",
		Sender:        "ccsvc@message.cmbchina.com",
		Subject:       "每日信用管家",
		Text:          "2026/09/07 08:30:00 CNY 12.34 尾号1234消费 早餐店(每日邮件)",
		ReceivedAt:    time.Date(2026, 9, 7, 9, 0, 0, 0, time.FixedZone("CST", 8*60*60)),
		Authenticated: true,
	}}}
	service := NewEmailBillImportService(
		&fakeEmailBillConfigProvider{config: config},
		&fakeEmailBillUserService{user: &models.User{Uid: 7}},
		transactions,
		func(*settings.EmailBillConfig, []emailbill.Parser) emailbill.Mailbox { return mailbox },
	)

	require.NoError(t, service.Import(core.NewNullContext()))
}

type fakeEmailBillMailbox struct {
	messages []emailbill.Message
}

func (m *fakeEmailBillMailbox) FetchRecent(context.Context) ([]emailbill.Message, error) {
	return m.messages, nil
}

func TestEmailBillImporterCreatesNativeTransactionOnlyOnce(t *testing.T) {
	config := &settings.Config{EmailBillConfig: &settings.EmailBillConfig{
		Enabled:            true,
		TargetUser:         "alice",
		CMBCreditAccountID: 101,
		CMBDebitAccountID:  102,
		ExpenseCategoryID:  201,
		IncomeCategoryID:   202,
		Timezone:           "Asia/Shanghai",
	}}
	transactions := &fakeEmailBillTransactionService{markers: make(map[string]bool)}
	mailbox := &fakeEmailBillMailbox{messages: []emailbill.Message{{
		Fingerprint:   "<bill-1@example.com>",
		Sender:        "ccsvc@message.cmbchina.com",
		Subject:       "每日信用管家",
		Text:          "2026/09/07 08:30:00 CNY 12.34 尾号1234消费 早餐店(每日邮件)",
		ReceivedAt:    time.Date(2026, 9, 7, 9, 0, 0, 0, time.FixedZone("CST", 8*60*60)),
		Authenticated: true,
	}}}
	service := NewEmailBillImportService(
		&fakeEmailBillConfigProvider{config: config},
		&fakeEmailBillUserService{user: &models.User{Uid: 7}},
		transactions,
		func(*settings.EmailBillConfig, []emailbill.Parser) emailbill.Mailbox { return mailbox },
	)

	require.NoError(t, service.Import(core.NewNullContext()))
	require.Len(t, transactions.created, 1)
	created := transactions.created[0]
	assert.Equal(t, int64(7), created.Uid)
	assert.Equal(t, models.TRANSACTION_DB_TYPE_EXPENSE, created.Type)
	assert.Equal(t, int64(101), created.AccountId)
	assert.Equal(t, int64(201), created.CategoryId)
	assert.Equal(t, int64(1234), created.Amount)
	assert.Equal(t, int16(480), created.TimezoneUtcOffset)
	assert.Contains(t, created.Comment, "早餐店")
	assert.Contains(t, created.Comment, "[ebk-mail:")

	require.NoError(t, service.Import(core.NewNullContext()))
	assert.Len(t, transactions.created, 1)
}
