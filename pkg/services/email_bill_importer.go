package services

import (
	"context"
	"crypto/sha256"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/mayswind/ezbookkeeping/pkg/core"
	"github.com/mayswind/ezbookkeeping/pkg/emailbill"
	"github.com/mayswind/ezbookkeeping/pkg/log"
	"github.com/mayswind/ezbookkeeping/pkg/models"
	"github.com/mayswind/ezbookkeeping/pkg/settings"
	"github.com/mayswind/ezbookkeeping/pkg/utils"
)

const maxTransactionCommentLength = 255

var emailBillMarkerPattern = regexp.MustCompile(`\[ebk-mail:[0-9a-f]{20}\]`)

type emailBillConfigProvider interface {
	GetCurrentConfig() *settings.Config
}

type emailBillUserService interface {
	GetUserByUsername(core.Context, string) (*models.User, error)
}

type emailBillTransactionService interface {
	HasEmailBillMarker(core.Context, int64, string) (bool, error)
	HasTransactionAtTime(core.Context, int64, int64) (bool, error)
	CreateEmailBillTransaction(core.Context, *models.Transaction) error
}

type emailBillMailboxFactory func(*settings.EmailBillConfig, []emailbill.Parser) emailbill.Mailbox

type emailBillAutomationRules interface {
	RunnableParserRules(core.Context, int64) ([]emailbill.RunnableParserRule, error)
}

type emailBillPipelineProcessor interface {
	ProcessMessage(core.Context, int64, int64, EmailBillFetchedMessage, []emailbill.RunnableParserRule) (*EmailBillPipelineResult, error)
}

type emailBillCandidateFinalizer interface {
	FinalizeMessage(core.Context, int64, int64, int64, int64) error
}

type emailBillRawBodyCleaner interface {
	PurgeRawBodies(core.Context, int64, int64) error
}

type emailBillMessageIdentityStore interface {
	HasMessageFingerprint(core.Context, int64, int64, string, uint16) (bool, error)
}

// EmailBillImportService imports supported email notifications into native transactions.
type EmailBillImportService struct {
	configProvider emailBillConfigProvider
	users          emailBillUserService
	transactions   emailBillTransactionService
	mailboxFactory emailBillMailboxFactory
	automation     emailBillAutomationRules
	pipeline       emailBillPipelineProcessor
	finalizer      emailBillCandidateFinalizer
	rawBodies      emailBillRawBodyCleaner
	messageStore   emailBillMessageIdentityStore
}

// EmailBillImporter is the built-in email bill importer service.
var EmailBillImporter = newConfiguredEmailBillImportService()

func newConfiguredEmailBillImportService() *EmailBillImportService {
	service := NewEmailBillImportService(settings.Container, Users, Transactions, newEmailBillMailbox)
	service.automation = EmailBillAutomation
	service.pipeline = NewEmailBillPipeline(EmailBillAutomationStore, emailbill.NewScriptParser(emailbill.ScriptLimits{}))
	service.finalizer = NewEmailBillFinalizer()
	service.rawBodies = EmailBillAutomationStore
	service.messageStore = EmailBillAutomationStore
	return service
}

// NewEmailBillImportService creates an email bill importer with injectable dependencies.
func NewEmailBillImportService(configProvider emailBillConfigProvider, users emailBillUserService, transactions emailBillTransactionService, mailboxFactory emailBillMailboxFactory) *EmailBillImportService {
	return &EmailBillImportService{
		configProvider: configProvider,
		users:          users,
		transactions:   transactions,
		mailboxFactory: mailboxFactory,
	}
}

// Import fetches supported bank emails and creates any missing transactions.
func (s *EmailBillImportService) Import(c core.Context) error {
	config := s.configProvider.GetCurrentConfig()
	if config == nil || config.EmailBillConfig == nil || !config.EmailBillConfig.Enabled {
		return nil
	}
	emailConfig := config.EmailBillConfig

	user, err := s.users.GetUserByUsername(c, emailConfig.TargetUser)
	if err != nil {
		return fmt.Errorf("resolve email bill target user: %w", err)
	}
	location, err := time.LoadLocation(emailConfig.Timezone)
	if err != nil {
		return fmt.Errorf("load email bill timezone: %w", err)
	}
	if s.automation != nil && s.pipeline != nil {
		return s.importWithAutomation(c, user.Uid, emailConfig)
	}

	parsers := []emailbill.Parser{emailbill.NewCMBCreditParser(), emailbill.NewCMBDebitParser()}
	messages, err := s.mailboxFactory(emailConfig, parsers).FetchRecent(c)
	if err != nil {
		return err
	}

	for _, message := range messages {
		for _, parser := range parsers {
			if !parser.Matches(message.Sender, message.Subject) {
				continue
			}
			parsed, parseErr := parser.Parse(message.Text, message.ReceivedAt.In(location))
			if parseErr != nil {
				return fmt.Errorf("parse email bill %q: %w", message.Fingerprint, parseErr)
			}
			for index, item := range parsed {
				marker := makeEmailBillMarker(message.Fingerprint, item, index)
				exists, existsErr := s.transactions.HasEmailBillMarker(c, user.Uid, marker)
				if existsErr != nil {
					return fmt.Errorf("check email bill marker: %w", existsErr)
				}
				if exists {
					continue
				}

				transaction := buildEmailBillTransaction(c, user.Uid, emailConfig, item, marker)
				if createErr := s.createTransaction(c, transaction, marker); createErr != nil {
					return createErr
				}
				log.Infof(c, "[email_bill_importer.Import] created transaction %d from %s email", transaction.TransactionId, item.Source)
			}
			break
		}
	}
	return nil
}

func (s *EmailBillImportService) importWithAutomation(c core.Context, uid int64, config *settings.EmailBillConfig) error {
	return s.importWithObserver(c, uid, config, nil)
}

func (s *EmailBillImportService) importWithObserver(c core.Context, uid int64, config *settings.EmailBillConfig, observer emailbill.ScanObserver) error {
	if s.rawBodies != nil {
		cutoff := int64(0)
		if config.RetainRawEmails {
			cutoff = time.Now().Add(-time.Duration(config.RawEmailRetentionDays) * 24 * time.Hour).Unix()
		}
		if err := s.rawBodies.PurgeRawBodies(c, uid, cutoff); err != nil {
			return fmt.Errorf("purge retained email bodies: %w", err)
		}
	}
	rules, err := s.automation.RunnableParserRules(c, uid)
	if err != nil {
		return fmt.Errorf("load email bill parser rules: %w", err)
	}
	mailbox := s.mailboxFactory(config, nil)
	if observable, ok := mailbox.(interface{ SetScanObserver(emailbill.ScanObserver) }); ok {
		observable.SetScanObserver(observer)
	}
	if matchable, ok := mailbox.(interface {
		SetParserMatchers([]emailbill.ParserMatcher)
	}); ok {
		matchers := make([]emailbill.ParserMatcher, 0, len(rules))
		for _, rule := range rules {
			matchers = append(matchers, rule.Matcher)
		}
		matchable.SetParserMatchers(matchers)
	}
	if filterable, ok := mailbox.(interface{ SetMessageFilter(emailbill.MessageFilter) }); ok && s.messageStore != nil {
		filterable.SetMessageFilter(func(_ context.Context, message emailbill.Message) (bool, error) {
			if strings.TrimSpace(message.MessageID) == "" {
				return true, nil
			}
			fingerprint, version := emailbill.MessageFingerprint(emailbill.MessageIdentityInput{
				MailboxID: uid, MessageID: message.MessageID, Sender: message.Sender, Subject: message.Subject,
				ReceivedAt: message.ReceivedAt,
			})
			exists, lookupErr := s.messageStore.HasMessageFingerprint(c, uid, uid, fingerprint, version)
			return !exists, lookupErr
		})
	}
	process := func(message emailbill.Message) error {
		if err := c.Err(); err != nil {
			return err
		}
		if observer != nil {
			if err := observer(emailbill.ScanEvent{Kind: "message", Message: &message, Folder: message.Folder, Status: "processing"}); err != nil {
				return err
			}
		}
		result, processErr := s.pipeline.ProcessMessage(c, uid, uid, EmailBillFetchedMessage{
			RemoteMessageID: message.MessageID, Sender: message.Sender, Subject: message.Subject,
			ReceivedAt: message.ReceivedAt, Text: message.Text, Headers: message.Headers,
			RetainBody: config.RetainRawEmails,
		}, rules)
		if processErr == nil && s.finalizer != nil && !result.Duplicate {
			processErr = s.finalizer.FinalizeMessage(c, uid, uid, result.MessageID, result.RunID)
		}
		if observer != nil {
			event := emailbill.ScanEvent{Kind: "message", Message: &message, Folder: message.Folder, Processed: true}
			if result != nil {
				event.MessageID, event.RunID, event.Status = result.MessageID, result.RunID, result.Status
			}
			if processErr != nil {
				event.Status, event.Reason = "failed", processErr.Error()
			}
			if err := observer(event); err != nil {
				return err
			}
			// Failure evidence is retained; continue with independent messages.
			return nil
		}
		return processErr
	}
	if streaming, ok := mailbox.(interface {
		SetMessageHandler(func(emailbill.Message) error)
	}); ok {
		streaming.SetMessageHandler(process)
		_, err = mailbox.FetchRecent(c)
		return err
	}
	messages, err := mailbox.FetchRecent(c)
	if err != nil {
		return err
	}
	for _, message := range messages {
		if err := process(message); err != nil {
			return err
		}
	}
	return nil
}

func (s *EmailBillImportService) createTransaction(c core.Context, transaction *models.Transaction, marker string) error {
	baseTime := utils.GetMinTransactionTimeFromUnixTime(utils.GetUnixTimeFromTransactionTime(transaction.TransactionTime))
	startOffset := emailBillTransactionTimeOffset(marker)

	for attempt := int64(0); attempt < 999; attempt++ {
		transaction.TransactionTime = baseTime + (startOffset+attempt)%999
		occupied, err := s.transactions.HasTransactionAtTime(c, transaction.Uid, transaction.TransactionTime)
		if err != nil {
			return fmt.Errorf("check email bill transaction time: %w", err)
		}
		if occupied {
			continue
		}

		createErr := s.transactions.CreateEmailBillTransaction(c, transaction)
		if createErr == nil {
			return nil
		}

		exists, recheckErr := s.transactions.HasEmailBillMarker(c, transaction.Uid, marker)
		if recheckErr != nil {
			return fmt.Errorf("create email bill transaction: %v; recheck marker: %w", createErr, recheckErr)
		}
		if exists {
			return nil
		}

		occupied, occupiedErr := s.transactions.HasTransactionAtTime(c, transaction.Uid, transaction.TransactionTime)
		if occupiedErr != nil {
			return fmt.Errorf("create email bill transaction: %v; recheck time: %w", createErr, occupiedErr)
		}
		if !occupied {
			return fmt.Errorf("create email bill transaction: %w", createErr)
		}
	}

	return fmt.Errorf("create email bill transaction: no free transaction time within the source second")
}

func newEmailBillMailbox(config *settings.EmailBillConfig, parsers []emailbill.Parser) emailbill.Mailbox {
	return emailbill.NewIMAPMailbox(emailbill.MailboxConfig{
		FolderMode:      config.FolderMode,
		Folders:         append([]string(nil), config.Folders...),
		Server:          config.IMAPServer,
		Port:            config.IMAPPort,
		Username:        config.MailUser,
		Password:        config.MailPassword,
		MaxEmails:       config.MaxEmails,
		MaxMessageBytes: config.MaxMessageBytes,
	}, parsers)
}

// TestConnection validates mailbox access without fetching or importing messages.
func (s *EmailBillImportService) TestConnection(c core.Context, config *settings.EmailBillConfig) error {
	if config == nil || strings.TrimSpace(config.IMAPServer) == "" || config.IMAPPort == 0 ||
		strings.TrimSpace(config.MailUser) == "" || config.MailPassword == "" {
		return fmt.Errorf("IMAP server, port, user and password are required")
	}
	mailbox := s.mailboxFactory(config, nil)
	tester, ok := mailbox.(interface{ TestConnection(context.Context) error })
	if !ok {
		return fmt.Errorf("mailbox connection test is not supported")
	}
	return tester.TestConnection(c)
}

// ListFolders discovers selectable directories without selecting or fetching messages.
func (s *EmailBillImportService) ListFolders(c core.Context, config *settings.EmailBillConfig) ([]string, error) {
	if config == nil || config.IMAPServer == "" || config.IMAPPort == 0 || config.MailUser == "" || config.MailPassword == "" {
		return nil, fmt.Errorf("IMAP server, port, user and password are required")
	}
	mailbox := s.mailboxFactory(config, nil)
	lister, ok := mailbox.(interface {
		ListFolders(context.Context) ([]string, error)
	})
	if !ok {
		return nil, fmt.Errorf("mailbox folder discovery is not supported")
	}
	return lister.ListFolders(c)
}

func buildEmailBillTransaction(c core.Context, uid int64, config *settings.EmailBillConfig, parsed emailbill.ParsedTransaction, marker string) *models.Transaction {
	transactionType := models.TRANSACTION_DB_TYPE_EXPENSE
	accountID := config.CMBCreditAccountID
	categoryID := config.ExpenseCategoryID
	amount := parsed.AmountMinor
	if amount < 0 {
		amount = -amount
	}
	if parsed.AmountMinor > 0 {
		transactionType = models.TRANSACTION_DB_TYPE_INCOME
		categoryID = config.IncomeCategoryID
	}
	if parsed.Source == emailbill.SourceCMBDebit {
		accountID = config.CMBDebitAccountID
	}

	commentPrefix := strings.TrimSpace(parsed.Merchant)
	if parsed.Description != "" && parsed.Description != parsed.Merchant {
		commentPrefix += " | " + strings.TrimSpace(parsed.Description)
	}
	comment := trimRunes(commentPrefix, maxTransactionCommentLength-len(marker)-1) + " " + marker

	return &models.Transaction{
		Uid:               uid,
		Type:              transactionType,
		CategoryId:        categoryID,
		AccountId:         accountID,
		TransactionTime:   utils.GetMinTransactionTimeFromUnixTime(parsed.OccurredAt.Unix()),
		TimezoneUtcOffset: utils.GetTimezoneOffsetMinutes(parsed.OccurredAt.Unix(), parsed.OccurredAt.Location()),
		Amount:            amount,
		Comment:           strings.TrimSpace(comment),
		CreatedIp:         c.ClientIP(),
	}
}

func makeEmailBillMarker(fingerprint string, parsed emailbill.ParsedTransaction, index int) string {
	value := fmt.Sprintf("%s\x00%s\x00%d\x00%d\x00%d\x00%s", fingerprint, parsed.Source, parsed.OccurredAt.Unix(), parsed.AmountMinor, index, parsed.Merchant)
	digest := sha256.Sum256([]byte(value))
	return fmt.Sprintf("[ebk-mail:%x]", digest[:10])
}

func emailBillMarker(comment string) string {
	return emailBillMarkerPattern.FindString(comment)
}

func emailBillTransactionTimeOffset(marker string) int64 {
	digest := sha256.Sum256([]byte(marker))
	return int64(uint16(digest[0])<<8|uint16(digest[1])) % 999
}

func trimRunes(value string, maximum int) string {
	runes := []rune(value)
	if len(runes) > maximum {
		runes = runes[:maximum]
	}
	return string(runes)
}
