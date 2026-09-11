package api

import (
	"sync"

	"github.com/mayswind/ezbookkeeping/pkg/core"
	"github.com/mayswind/ezbookkeeping/pkg/services"
	"github.com/mayswind/ezbookkeeping/pkg/settings"
)

// Serializes configuration snapshots, persistence and runtime publication across settings APIs.
var settingsUpdateMutex sync.Mutex

func canManageGlobalSettings(c *core.WebContext, config *settings.Config) bool {
	if config == nil || config.SettingsManagementUser == "" {
		return false
	}
	user, err := services.Users.GetUserById(c, c.GetCurrentUid())
	return err == nil && user.Username == config.SettingsManagementUser
}

func canManageEmailSettings(config *settings.Config, username string) bool {
	if config == nil {
		return false
	}
	if config.EmailBillConfig != nil && config.EmailBillConfig.TargetUser != "" {
		return config.EmailBillConfig.TargetUser == username
	}
	return config.SettingsManagementUser != "" && config.SettingsManagementUser == username
}
