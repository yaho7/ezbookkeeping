package services

import (
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
	CreateTransaction(core.Context, *models.Transaction, []int64, []int64) error
}

type emailBillMailboxFactory func(*settings.EmailBillConfig, []emailbill.Parser) emailbill.Mailbox

// EmailBillImportService imports supported email notifications into native transactions.
type EmailBillImportService struct {
	configProvider emailBillConfigProvider
	users          emailBillUserService
	transactions   emailBillTransactionService
	mailboxFactory emailBillMailboxFactory
}

// EmailBillImporter is the built-in email bill importer service.
var EmailBillImporter = NewEmailBillImportService(settings.Container, Users, Transactions, newEmailBillMailbox)

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

	parsers := []emailbill.Parser{emailbill.NewCMBCreditParser(), emailbill.NewCMBDebitParser()}
	messages, err := s.mailboxFactory(emailConfig, parsers).FetchRecent(c)
	if err != nil {
		return err
	}

	for _, message := range messages {
		if !message.Authenticated {
			log.Warnf(c, "[email_bill_importer.Import] skipped unauthenticated email %q", message.Fingerprint)
			continue
		}
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
				if createErr := s.transactions.CreateTransaction(c, transaction, nil, nil); createErr != nil {
					exists, recheckErr := s.transactions.HasEmailBillMarker(c, user.Uid, marker)
					if recheckErr == nil && exists {
						continue
					}
					if recheckErr != nil {
						return fmt.Errorf("create email bill transaction: %v; recheck marker: %w", createErr, recheckErr)
					}
					return fmt.Errorf("create email bill transaction: %w", createErr)
				}
				log.Infof(c, "[email_bill_importer.Import] created transaction %d from %s email", transaction.TransactionId, item.Source)
			}
			break
		}
	}
	return nil
}

func newEmailBillMailbox(config *settings.EmailBillConfig, parsers []emailbill.Parser) emailbill.Mailbox {
	return emailbill.NewIMAPMailbox(emailbill.MailboxConfig{
		Server:    config.IMAPServer,
		Port:      config.IMAPPort,
		Username:  config.MailUser,
		Password:  config.MailPassword,
		MaxEmails: config.MaxEmails,
		Security: emailbill.MessageSecurity{
			RequireAuthenticationResults: config.RequireAuthenticationResults,
			TrustedAuthservDomains:       config.TrustedAuthservDomains,
		},
	}, parsers)
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

func trimRunes(value string, maximum int) string {
	runes := []rune(value)
	if len(runes) > maximum {
		runes = runes[:maximum]
	}
	return string(runes)
}
