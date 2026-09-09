package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mayswind/ezbookkeeping/pkg/core"
	"github.com/mayswind/ezbookkeeping/pkg/llm/data"
	"github.com/mayswind/ezbookkeeping/pkg/models"
	"github.com/mayswind/ezbookkeeping/pkg/settings"
)

type fakeLLMTestProvider struct {
	content string
	err     error
}

func (p *fakeLLMTestProvider) GetJsonResponse(core.Context, int64, *settings.LLMConfig, *data.LargeLanguageModelRequest) (*data.LargeLanguageModelTextualResponse, error) {
	return &data.LargeLanguageModelTextualResponse{Content: p.content}, p.err
}

func TestBuildTextRecognitionLLMConfigPreservesBlankSecret(t *testing.T) {
	current := &settings.LLMConfig{
		LLMProvider:            settings.OpenAICompatibleLLMProvider,
		OpenAICompatibleAPIKey: "stored-secret",
	}
	request := &models.LLMSettingsUpdateRequest{
		Provider: settings.OpenAICompatibleLLMProvider, Endpoint: "https://llm.example.com/v1",
		ModelID: "model-a", Thinking: string(settings.LLMThinkingMedium), RequestTimeout: 90000,
		Proxy: "system",
	}

	actual, err := buildTextRecognitionLLMConfig(request, current)

	require.NoError(t, err)
	assert.Equal(t, "stored-secret", actual.OpenAICompatibleAPIKey)
	assert.Equal(t, "https://llm.example.com/v1", actual.OpenAICompatibleBaseURL)
	assert.Equal(t, "model-a", actual.OpenAICompatibleModelID)
	assert.Equal(t, settings.LLMThinkingMedium, actual.EnableThinking)
}

func TestBuildTextRecognitionLLMConfigMapsOllamaWithoutAPIKey(t *testing.T) {
	actual, err := buildTextRecognitionLLMConfig(&models.LLMSettingsUpdateRequest{
		Provider: settings.OllamaLLMProvider, Endpoint: "http://ollama:11434", ModelID: "qwen3:8b",
	}, nil)

	require.NoError(t, err)
	assert.Equal(t, "http://ollama:11434", actual.OllamaServerURL)
	assert.Equal(t, "qwen3:8b", actual.OllamaModelID)
	assert.Equal(t, uint32(60000), actual.LargeLanguageModelAPIRequestTimeout)
	assert.Equal(t, "system", actual.LargeLanguageModelAPIProxy)
}

func TestBuildTextRecognitionLLMConfigRejectsMissingEndpoint(t *testing.T) {
	_, err := buildTextRecognitionLLMConfig(&models.LLMSettingsUpdateRequest{
		Provider: settings.OpenAICompatibleLLMProvider, ModelID: "model-a", APIKey: "secret",
	}, nil)

	require.Error(t, err)
}

func TestLLMSettingsResponseRedactsSecret(t *testing.T) {
	response := textRecognitionLLMSettingsResponse(&settings.LLMConfig{
		LLMProvider: settings.GoogleAILLMProvider, GoogleAIAPIKey: "secret", GoogleAIModelID: "gemini",
	})

	assert.True(t, response.APIKeyConfigured)
	assert.Equal(t, "gemini", response.ModelID)
}

func TestTextRecognitionLLMConnectionAcceptsExpectedResponse(t *testing.T) {
	err := testTextRecognitionLLM(core.NewNullContext(), 7, &settings.LLMConfig{}, &fakeLLMTestProvider{content: `{"status":"ok"}`})

	require.NoError(t, err)
}

func TestTextRecognitionLLMConnectionRejectsUnexpectedResponse(t *testing.T) {
	err := testTextRecognitionLLM(core.NewNullContext(), 7, &settings.LLMConfig{}, &fakeLLMTestProvider{content: `{"status":"maybe"}`})

	require.ErrorContains(t, err, "unexpected")
}
