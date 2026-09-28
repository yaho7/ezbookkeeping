package api

import (
	"github.com/mayswind/ezbookkeeping/pkg/core"
	"github.com/mayswind/ezbookkeeping/pkg/errs"
	"github.com/mayswind/ezbookkeeping/pkg/models"
	"github.com/mayswind/ezbookkeeping/pkg/services"
	"github.com/mayswind/ezbookkeeping/pkg/settings"
)

// NotificationPreviewHandler renders the sending template using sample data only.
func (a *EmailBillSettingsApi) NotificationPreviewHandler(c *core.WebContext) (any, *errs.Error) {
	request := &struct {
		Outcome string `json:"outcome" binding:"required,oneof=succeeded failed empty"`
	}{}
	if err := c.ShouldBindJSON(request); err != nil {
		return nil, errs.NewIncompleteOrIncorrectSubmissionError(err)
	}
	user, err := a.users.GetUserById(c, c.GetCurrentUid())
	if err != nil {
		return nil, errs.Or(err, errs.ErrUserNotFound)
	}
	config := a.container.GetCurrentConfig()
	if config == nil || !canManageEmailSettings(config, user.Username) {
		return nil, errs.ErrNotPermittedToPerformThisAction
	}
	preview, err := services.EmailBillNotifications.Preview(config.RootUrl, user.Language, request.Outcome)
	if err != nil {
		return nil, errs.Or(err, errs.ErrOperationFailed)
	}
	return preview, nil
}

// NotificationTestHandler sends a test only on an explicit authenticated request.
func (a *EmailBillSettingsApi) NotificationTestHandler(c *core.WebContext) (any, *errs.Error) {
	request := &models.EmailBillNotificationSettingsRequest{}
	if err := c.ShouldBindJSON(request); err != nil {
		return nil, errs.NewIncompleteOrIncorrectSubmissionError(err)
	}
	user, err := a.users.GetUserById(c, c.GetCurrentUid())
	if err != nil {
		return nil, errs.Or(err, errs.ErrUserNotFound)
	}
	config := a.container.GetCurrentConfig()
	if config == nil || config.EmailBillConfig == nil || !canManageEmailSettings(config, user.Username) {
		return nil, errs.ErrNotPermittedToPerformThisAction
	}
	emailConfig := *config.EmailBillConfig
	emailConfig.Notification = buildEmailBillNotificationConfig(request, config.EmailBillConfig.Notification)
	if err = settings.NormalizeEmailBillNotification(&emailConfig, true); err == nil {
		err = services.EmailBillNotifications.Test(&emailConfig, config.RootUrl, user.Language)
	}
	if err != nil {
		return map[string]any{"sent": false, "error": services.SanitizeEmailBillNotificationError(err.Error(), &emailConfig)}, nil
	}
	return map[string]any{"sent": true, "error": ""}, nil
}
