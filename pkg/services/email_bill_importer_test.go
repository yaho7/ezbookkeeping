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
	"github.com/mayswind/ezbookkeeping/pkg/utils"
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
	occupiedTimes   map[int64]bool
	createErr       error
	markBeforeError bool
}

func (s *fakeEmailBillTransactionService) HasEmailBillMarker(_ core.Context, _ int64, marker string) (bool, error) {
	return s.markers[marker], nil
}

func (s *fakeEmailBillTransactionService) HasTransactionAtTime(_ core.Context, _ int64, transactionTime int64) (bool, error) {
	return s.occupiedTimes[transactionTime], nil
}

func (s *fakeEmailBillTransactionService) CreateEmailBillTransaction(_ core.Context, transaction *models.Transaction) error {
	if s.createErr != nil {
		if s.markBeforeError {
			s.markers[emailBillMarker(transaction.Comment)] = true
		}
		return s.createErr
	}
	s.created = append(s.created, transaction)
	s.markers[emailBillMarker(transaction.Comment)] = true
	s.occupiedTimes[transaction.TransactionTime] = true
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
		occupiedTimes:   make(map[int64]bool),
		createErr:       errors.New("duplicate transaction time"),
		markBeforeError: true,
	}
	mailbox := &fakeEmailBillMailbox{messages: []emailbill.Message{{
		Fingerprint: "<bill-race@example.com>",
		Sender:      "ccsvc@message.cmbchina.com",
		Subject:     "每日信用管家",
		Text:        "2026/09/07 08:30:00 CNY 12.34 尾号1234消费 早餐店(每日邮件)",
		ReceivedAt:  time.Date(2026, 9, 7, 9, 0, 0, 0, time.FixedZone("CST", 8*60*60)),
	}}}
	service := NewEmailBillImportService(
		&fakeEmailBillConfigProvider{config: config},
		&fakeEmailBillUserService{user: &models.User{Uid: 7}},
		transactions,
		func(*settings.EmailBillConfig, []emailbill.Parser) emailbill.Mailbox { return mailbox },
	)

	require.NoError(t, service.Import(core.NewNullContext()))
}

func TestEmailBillImporterUsesNextDeterministicTimeWhenOccupied(t *testing.T) {
	marker := "[ebk-mail:0123456789abcdef0123]"
	baseTime := utils.GetMinTransactionTimeFromUnixTime(time.Date(2026, 9, 7, 8, 30, 0, 0, time.UTC).Unix())
	firstTime := baseTime + emailBillTransactionTimeOffset(marker)
	transactions := &fakeEmailBillTransactionService{
		markers:       make(map[string]bool),
		occupiedTimes: map[int64]bool{firstTime: true},
	}
	service := &EmailBillImportService{transactions: transactions}
	transaction := &models.Transaction{Uid: 7, TransactionTime: baseTime, Comment: marker}

	require.NoError(t, service.createTransaction(core.NewNullContext(), transaction, marker))
	require.Len(t, transactions.created, 1)
	assert.Equal(t, baseTime+(emailBillTransactionTimeOffset(marker)+1)%999, transactions.created[0].TransactionTime)
}

type fakeEmailBillMailbox struct {
	messages      []emailbill.Message
	connectionErr error
	skipMessage   emailbill.MessageFilter
}

type streamingEmailBillMailbox struct {
	fakeEmailBillMailbox
	handler    func(emailbill.Message) error
	afterBatch error
}

func (m *streamingEmailBillMailbox) SetMessageHandler(handler func(emailbill.Message) error) {
	m.handler = handler
}

func (m *streamingEmailBillMailbox) FetchRecent(context.Context) ([]emailbill.Message, error) {
	for _, message := range m.messages {
		if err := m.handler(message); err != nil {
			return nil, err
		}
	}
	return m.messages, m.afterBatch
}

func TestEmailBillStreamingPersistsBeforeLaterScanFailure(t *testing.T) {
	scanErr := errors.New("second folder disconnected")
	for _, afterBatch := range []error{nil, scanErr} {
		mailbox := &streamingEmailBillMailbox{fakeEmailBillMailbox: fakeEmailBillMailbox{messages: []emailbill.Message{{MessageID: "first"}}}, afterBatch: afterBatch}
		pipeline := &fakeEmailBillPipelineProcessor{}
		service := &EmailBillImportService{
			automation: &fakeEmailBillAutomationRules{}, pipeline: pipeline,
			mailboxFactory: func(*settings.EmailBillConfig, []emailbill.Parser) emailbill.Mailbox { return mailbox },
		}
		processed := 0
		err := service.importWithObserver(core.NewNullContext(), 7, &settings.EmailBillConfig{}, func(event emailbill.ScanEvent) error {
			if event.Processed {
				processed++
			}
			return nil
		})
		require.ErrorIs(t, err, afterBatch)
		require.Len(t, pipeline.processed, 1, "streamed messages must not be reprocessed from the return value")
		require.Equal(t, 1, processed)
	}
}

type fakeEmailBillAutomationRules struct {
	rules []emailbill.RunnableParserRule
}

func (s *fakeEmailBillAutomationRules) RunnableParserRules(core.Context, int64) ([]emailbill.RunnableParserRule, error) {
	return s.rules, nil
}

type fakeEmailBillPipelineProcessor struct {
	processed []EmailBillFetchedMessage
}

type fakeEmailBillCandidateFinalizer struct {
	calls int
}

func (f *fakeEmailBillCandidateFinalizer) FinalizeMessage(core.Context, int64, int64, int64, int64) error {
	f.calls++
	return nil
}

func (p *fakeEmailBillPipelineProcessor) ProcessMessage(_ core.Context, _, _ int64, message EmailBillFetchedMessage, _ []emailbill.RunnableParserRule) (*EmailBillPipelineResult, error) {
	p.processed = append(p.processed, message)
	return &EmailBillPipelineResult{Status: "succeeded"}, nil
}

func TestEmailBillImporterUsesConfigurableParserPipeline(t *testing.T) {
	config := &settings.Config{EmailBillConfig: &settings.EmailBillConfig{
		Enabled: true, TargetUser: "alice", Timezone: "Asia/Shanghai",
	}}
	mailbox := &fakeEmailBillMailbox{messages: []emailbill.Message{{
		MessageID: "<one@example.com>", Sender: "bank@example.com", Subject: "bill", Text: "body",
		ReceivedAt: time.Date(2026, 9, 8, 8, 0, 0, 0, time.UTC),
	}}}
	pipeline := &fakeEmailBillPipelineProcessor{}
	finalizer := &fakeEmailBillCandidateFinalizer{}
	service := NewEmailBillImportService(
		&fakeEmailBillConfigProvider{config: config}, &fakeEmailBillUserService{user: &models.User{Uid: 7}},
		&fakeEmailBillTransactionService{}, func(*settings.EmailBillConfig, []emailbill.Parser) emailbill.Mailbox { return mailbox },
	)
	service.automation = &fakeEmailBillAutomationRules{rules: []emailbill.RunnableParserRule{{VersionID: 9}}}
	service.pipeline = pipeline
	service.finalizer = finalizer

	err := service.Import(core.NewNullContext())
	require.NoError(t, err)
	require.Len(t, pipeline.processed, 1)
	assert.Equal(t, "<one@example.com>", pipeline.processed[0].RemoteMessageID)
	assert.Equal(t, 1, finalizer.calls)
}

func (m *fakeEmailBillMailbox) FetchRecent(context.Context) ([]emailbill.Message, error) {
	return m.messages, nil
}

func (m *fakeEmailBillMailbox) TestConnection(context.Context) error { return m.connectionErr }

func (m *fakeEmailBillMailbox) SetMessageFilter(filter emailbill.MessageFilter) {
	m.skipMessage = filter
}

func TestEmailBillImporterTestsConnectionWithoutImporting(t *testing.T) {
	mailbox := &fakeEmailBillMailbox{}
	service := NewEmailBillImportService(nil, nil, nil, func(*settings.EmailBillConfig, []emailbill.Parser) emailbill.Mailbox { return mailbox })

	err := service.TestConnection(core.NewNullContext(), &settings.EmailBillConfig{
		IMAPServer: "imap.example.com", IMAPPort: 993, MailUser: "alice@example.com", MailPassword: "secret",
	})

	require.NoError(t, err)
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
	transactions := &fakeEmailBillTransactionService{markers: make(map[string]bool), occupiedTimes: make(map[int64]bool)}
	mailbox := &fakeEmailBillMailbox{messages: []emailbill.Message{{
		Fingerprint: "<bill-1@example.com>",
		Sender:      "ccsvc@message.cmbchina.com",
		Subject:     "每日信用管家",
		Text:        "2026/09/07 08:30:00 CNY 12.34 尾号1234消费 早餐店(每日邮件)",
		ReceivedAt:  time.Date(2026, 9, 7, 9, 0, 0, 0, time.FixedZone("CST", 8*60*60)),
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
