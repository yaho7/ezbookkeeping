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

func TestConfigContainerUpdateTextRecognitionLLMConfigUsesCopyOnWrite(t *testing.T) {
	originalLLMConfig := &LLMConfig{LLMProvider: OpenAILLMProvider, OpenAIModelID: "gpt-old"}
	original := &Config{TextRecognitionLLMConfig: originalLLMConfig}
	container := &ConfigContainer{current: original}

	updatedLLMConfig := &LLMConfig{LLMProvider: OpenAILLMProvider, OpenAIModelID: "gpt-new"}
	require.NoError(t, container.UpdateTextRecognitionLLMConfig(updatedLLMConfig))

	updated := container.GetCurrentConfig()
	assert.NotSame(t, original, updated)
	assert.Same(t, originalLLMConfig, original.TextRecognitionLLMConfig)
	assert.Equal(t, "gpt-new", updated.TextRecognitionLLMConfig.OpenAIModelID)

	updatedLLMConfig.OpenAIModelID = "mutated"
	assert.Equal(t, "gpt-new", container.GetCurrentConfig().TextRecognitionLLMConfig.OpenAIModelID)
}
