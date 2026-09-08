package settings

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigContainerUpdateEmailBillConfigUsesCopyOnWrite(t *testing.T) {
	originalEmailConfig := &EmailBillConfig{Enabled: false}
	original := &Config{EmailBillConfig: originalEmailConfig}
	container := &ConfigContainer{current: original}

	updatedEmailConfig := &EmailBillConfig{Enabled: true, MailUser: "alice@qq.com"}
	require.NoError(t, container.UpdateEmailBillConfig(updatedEmailConfig))

	updated := container.GetCurrentConfig()
	assert.NotSame(t, original, updated)
	assert.Same(t, originalEmailConfig, original.EmailBillConfig)
	assert.True(t, updated.EmailBillConfig.Enabled)
	assert.Equal(t, "alice@qq.com", updated.EmailBillConfig.MailUser)

	updatedEmailConfig.MailUser = "mutated@example.com"
	assert.Equal(t, "alice@qq.com", container.GetCurrentConfig().EmailBillConfig.MailUser)
}
