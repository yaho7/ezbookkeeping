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
	container *settings.ConfigContainer
	users     *services.UserService
}

// EmailBillSettings is the email bill settings api singleton.
var EmailBillSettings = &EmailBillSettingsApi{
	container: settings.Container,
	users:     services.Users,
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
		Timezone:                     request.Timezone,
		CronExpression:               request.CronExpression,
		MaxEmails:                    request.MaxEmails,
		MaxMessageBytes:              maxMessageBytes,
		RequireAuthenticationResults: request.RequireAuthenticationResults,
		TrustedAuthservDomains:       append([]string(nil), request.TrustedAuthservDomains...),
		RetainRawEmails:              request.RetainRawEmails,
		RawEmailRetentionDays:        request.RawEmailRetentionDays,
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
		Timezone:                     config.Timezone,
		CronExpression:               config.CronExpression,
		MaxEmails:                    config.MaxEmails,
		RequireAuthenticationResults: config.RequireAuthenticationResults,
		TrustedAuthservDomains:       append([]string(nil), config.TrustedAuthservDomains...),
		RetainRawEmails:              config.RetainRawEmails,
		RawEmailRetentionDays:        config.RawEmailRetentionDays,
	}
}
