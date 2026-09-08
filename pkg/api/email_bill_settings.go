package api

import (
	"strings"

	"github.com/mayswind/ezbookkeeping/pkg/core"
	"github.com/mayswind/ezbookkeeping/pkg/cron"
	"github.com/mayswind/ezbookkeeping/pkg/errs"
	"github.com/mayswind/ezbookkeeping/pkg/log"
	"github.com/mayswind/ezbookkeeping/pkg/models"
	"github.com/mayswind/ezbookkeeping/pkg/services"
	"github.com/mayswind/ezbookkeeping/pkg/settings"
)

// EmailBillSettingsApi manages the built-in email bill importer for its owning user.
type EmailBillSettingsApi struct {
	container  *settings.ConfigContainer
	users      *services.UserService
	accounts   *services.AccountService
	categories *services.TransactionCategoryService
}

// EmailBillSettings is the email bill settings api singleton.
var EmailBillSettings = &EmailBillSettingsApi{
	container:  settings.Container,
	users:      services.Users,
	accounts:   services.Accounts,
	categories: services.TransactionCategories,
}

// GetHandler returns email bill settings without exposing the mailbox password.
func (a *EmailBillSettingsApi) GetHandler(c *core.WebContext) (any, *errs.Error) {
	user, err := a.users.GetUserById(c, c.GetCurrentUid())
	if err != nil {
		return nil, errs.Or(err, errs.ErrUserNotFound)
	}

	config := a.container.GetCurrentConfig()
	if config == nil || config.EmailBillConfig == nil {
		return emailBillSettingsResponse(&settings.EmailBillConfig{}), nil
	}
	if config.EmailBillConfig.TargetUser != "" && config.EmailBillConfig.TargetUser != user.Username {
		return nil, errs.ErrNotPermittedToPerformThisAction
	}
	return emailBillSettingsResponse(config.EmailBillConfig), nil
}

// UpdateHandler validates, persists and immediately applies email bill settings.
func (a *EmailBillSettingsApi) UpdateHandler(c *core.WebContext) (any, *errs.Error) {
	request := &models.EmailBillSettingsUpdateRequest{}
	if err := c.ShouldBindJSON(request); err != nil {
		return false, errs.NewIncompleteOrIncorrectSubmissionError(err)
	}

	uid := c.GetCurrentUid()
	user, err := a.users.GetUserById(c, uid)
	if err != nil {
		return false, errs.Or(err, errs.ErrUserNotFound)
	}
	currentConfig := a.container.GetCurrentConfig()
	if currentConfig == nil {
		return false, errs.ErrOperationFailed
	}
	currentEmailConfig := currentConfig.EmailBillConfig
	if currentEmailConfig != nil && currentEmailConfig.TargetUser != "" && currentEmailConfig.TargetUser != user.Username {
		return false, errs.ErrNotPermittedToPerformThisAction
	}

	emailConfig, err := buildEmailBillConfig(user.Username, request, currentEmailConfig)
	if err != nil {
		log.Warnf(c, "[email_bill_settings.UpdateHandler] invalid settings for user \"uid:%d\", because %s", uid, err.Error())
		return false, errs.NewIncompleteOrIncorrectSubmissionError(err)
	}
	if emailConfig.Enabled {
		if err = a.validateMappings(c, uid, emailConfig); err != nil {
			return false, errs.Or(err, errs.ErrOperationFailed)
		}
	}

	if err = settings.SaveEmailBillConfiguration(currentConfig.ConfigFilePath, emailConfig); err != nil {
		log.Errorf(c, "[email_bill_settings.UpdateHandler] failed to persist settings, because %s", err.Error())
		return false, errs.ErrOperationFailed
	}
	if err = a.container.UpdateEmailBillConfig(emailConfig); err != nil {
		return false, errs.ErrOperationFailed
	}
	if err = cron.Container.UpdateEmailBillImportJob(c, emailConfig); err != nil {
		log.Errorf(c, "[email_bill_settings.UpdateHandler] failed to update cron job, because %s", err.Error())
		return false, errs.ErrOperationFailed
	}
	return emailBillSettingsResponse(emailConfig), nil
}

// RunHandler immediately executes one email bill import using the saved settings.
func (a *EmailBillSettingsApi) RunHandler(c *core.WebContext) (any, *errs.Error) {
	user, err := a.users.GetUserById(c, c.GetCurrentUid())
	if err != nil {
		return false, errs.Or(err, errs.ErrUserNotFound)
	}
	config := a.container.GetCurrentConfig()
	if config == nil || config.EmailBillConfig == nil || config.EmailBillConfig.TargetUser != user.Username {
		return false, errs.ErrNotPermittedToPerformThisAction
	}
	if err = cron.Container.SyncRunJobNow("ImportEmailBills"); err != nil {
		return false, errs.Or(err, errs.ErrOperationFailed)
	}
	return true, nil
}

func (a *EmailBillSettingsApi) validateMappings(c core.Context, uid int64, config *settings.EmailBillConfig) error {
	for _, accountID := range []int64{config.CMBCreditAccountID, config.CMBDebitAccountID} {
		if _, err := a.accounts.GetAccountByAccountId(c, uid, accountID); err != nil {
			return err
		}
	}
	expenseCategory, err := a.categories.GetCategoryByCategoryId(c, uid, config.ExpenseCategoryID)
	if err != nil {
		return err
	}
	if expenseCategory.Type != models.CATEGORY_TYPE_EXPENSE {
		return errs.ErrTransactionCategoryNotFound
	}
	incomeCategory, err := a.categories.GetCategoryByCategoryId(c, uid, config.IncomeCategoryID)
	if err != nil {
		return err
	}
	if incomeCategory.Type != models.CATEGORY_TYPE_INCOME {
		return errs.ErrTransactionCategoryNotFound
	}
	return nil
}

func buildEmailBillConfig(username string, request *models.EmailBillSettingsUpdateRequest, current *settings.EmailBillConfig) (*settings.EmailBillConfig, error) {
	password := strings.TrimSpace(request.MailPassword)
	maxMessageBytes := uint32(2 * 1024 * 1024)
	if current != nil {
		if password == "" {
			password = current.MailPassword
		}
		if current.MaxMessageBytes > 0 {
			maxMessageBytes = current.MaxMessageBytes
		}
	}

	config := &settings.EmailBillConfig{
		Enabled:                      request.Enabled,
		TargetUser:                   username,
		IMAPServer:                   request.IMAPServer,
		IMAPPort:                     request.IMAPPort,
		MailUser:                     request.MailUser,
		MailPassword:                 password,
		CMBCreditAccountID:           request.CMBCreditAccountID,
		CMBDebitAccountID:            request.CMBDebitAccountID,
		ExpenseCategoryID:            request.ExpenseCategoryID,
		IncomeCategoryID:             request.IncomeCategoryID,
		Timezone:                     request.Timezone,
		CronExpression:               request.CronExpression,
		MaxEmails:                    request.MaxEmails,
		MaxMessageBytes:              maxMessageBytes,
		RequireAuthenticationResults: request.RequireAuthenticationResults,
		TrustedAuthservDomains:       append([]string(nil), request.TrustedAuthservDomains...),
	}
	return config, settings.NormalizeEmailBillConfiguration(config)
}

func emailBillSettingsResponse(config *settings.EmailBillConfig) *models.EmailBillSettingsResponse {
	return &models.EmailBillSettingsResponse{
		Enabled:                      config.Enabled,
		IMAPServer:                   config.IMAPServer,
		IMAPPort:                     config.IMAPPort,
		MailUser:                     config.MailUser,
		PasswordConfigured:           config.MailPassword != "",
		CMBCreditAccountID:           config.CMBCreditAccountID,
		CMBDebitAccountID:            config.CMBDebitAccountID,
		ExpenseCategoryID:            config.ExpenseCategoryID,
		IncomeCategoryID:             config.IncomeCategoryID,
		Timezone:                     config.Timezone,
		CronExpression:               config.CronExpression,
		MaxEmails:                    config.MaxEmails,
		RequireAuthenticationResults: config.RequireAuthenticationResults,
		TrustedAuthservDomains:       append([]string(nil), config.TrustedAuthservDomains...),
	}
}
