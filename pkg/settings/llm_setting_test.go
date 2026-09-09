package settings

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/ini.v1"
)

func TestSaveTextRecognitionLLMConfigurationPreservesOtherSections(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "ezbookkeeping.ini")
	require.NoError(t, os.WriteFile(configPath, []byte("[server]\nhttp_port = 8080\n"), 0o600))
	config := &LLMConfig{
		LLMProvider: OpenAICompatibleLLMProvider, EnableThinking: LLMThinkingMedium,
		OpenAICompatibleBaseURL: "https://llm.example.com/v1", OpenAICompatibleAPIKey: "secret",
		OpenAICompatibleModelID: "model-a", LargeLanguageModelAPIRequestTimeout: 90000,
		LargeLanguageModelAPIProxy: "system", LargeLanguageModelAPISkipTLSVerify: true,
	}

	require.NoError(t, SaveTextRecognitionLLMConfiguration(configPath, config))

	configFile, err := ini.Load(configPath)
	require.NoError(t, err)
	assert.Equal(t, "8080", configFile.Section("server").Key("http_port").String())
	section := configFile.Section("llm_text_recognition")
	assert.Equal(t, OpenAICompatibleLLMProvider, section.Key("llm_provider").String())
	assert.Equal(t, "secret", section.Key("openai_compatible_api_key").String())
	assert.Equal(t, "90000", section.Key("request_timeout").String())
	assert.Equal(t, "true", section.Key("skip_tls_verify").String())
	fileInfo, err := os.Stat(configPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), fileInfo.Mode().Perm())
}
