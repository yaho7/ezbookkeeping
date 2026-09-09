package api

import (
	"encoding/json"
	"fmt"
	"net/url"
	"reflect"
	"strings"

	"github.com/mayswind/ezbookkeeping/pkg/core"
	"github.com/mayswind/ezbookkeeping/pkg/errs"
	"github.com/mayswind/ezbookkeeping/pkg/llm"
	"github.com/mayswind/ezbookkeeping/pkg/llm/data"
	llmprovider "github.com/mayswind/ezbookkeeping/pkg/llm/provider"
	"github.com/mayswind/ezbookkeeping/pkg/log"
	"github.com/mayswind/ezbookkeeping/pkg/models"
	"github.com/mayswind/ezbookkeeping/pkg/settings"
)

const defaultLLMRequestTimeout uint32 = 60000

// LLMSettingsApi manages the global text-recognition LLM used by application AI features.
type LLMSettingsApi struct {
	container *settings.ConfigContainer
}

// LLMSettings is the application LLM settings API singleton.
var LLMSettings = &LLMSettingsApi{container: settings.Container}

// GetHandler returns the global text LLM configuration without its credential.
func (a *LLMSettingsApi) GetHandler(_ *core.WebContext) (any, *errs.Error) {
	config := a.container.GetCurrentConfig()
	if config == nil {
		return nil, errs.ErrOperationFailed
	}
	return textRecognitionLLMSettingsResponse(config.TextRecognitionLLMConfig), nil
}

// UpdateHandler persists and immediately applies the global text LLM configuration.
func (a *LLMSettingsApi) UpdateHandler(c *core.WebContext) (any, *errs.Error) {
	request := &models.LLMSettingsUpdateRequest{}
	if err := c.ShouldBindJSON(request); err != nil {
		return false, errs.NewIncompleteOrIncorrectSubmissionError(err)
	}
	currentConfig := a.container.GetCurrentConfig()
	if currentConfig == nil {
		return false, errs.ErrOperationFailed
	}
	llmConfig, err := buildTextRecognitionLLMConfig(request, currentConfig.TextRecognitionLLMConfig)
	if err != nil {
		return false, errs.NewIncompleteOrIncorrectSubmissionError(err)
	}
	if err = settings.SaveTextRecognitionLLMConfiguration(currentConfig.ConfigFilePath, llmConfig); err != nil {
		log.Errorf(c, "[llm_settings.UpdateHandler] failed to persist settings, because %s", err.Error())
		return false, errs.ErrOperationFailed
	}
	if err = a.container.UpdateTextRecognitionLLMConfig(llmConfig); err != nil {
		return false, errs.ErrOperationFailed
	}
	if err = llm.InitializeLargeLanguageModelProvider(a.container.GetCurrentConfig()); err != nil {
		log.Errorf(c, "[llm_settings.UpdateHandler] failed to initialize provider, because %s", err.Error())
		return false, errs.Or(err, errs.ErrOperationFailed)
	}
	return textRecognitionLLMSettingsResponse(llmConfig), nil
}

// TestHandler tests the submitted settings without saving or replacing the active provider.
func (a *LLMSettingsApi) TestHandler(c *core.WebContext) (any, *errs.Error) {
	request := &models.LLMSettingsUpdateRequest{}
	if err := c.ShouldBindJSON(request); err != nil {
		return false, errs.NewIncompleteOrIncorrectSubmissionError(err)
	}
	currentConfig := a.container.GetCurrentConfig()
	if currentConfig == nil {
		return false, errs.ErrOperationFailed
	}
	llmConfig, err := buildTextRecognitionLLMConfig(request, currentConfig.TextRecognitionLLMConfig)
	if err != nil || llmConfig.LLMProvider == "" {
		if err == nil {
			err = fmt.Errorf("LLM provider is required")
		}
		return false, errs.NewIncompleteOrIncorrectSubmissionError(err)
	}
	provider, err := llm.NewLargeLanguageModelProvider(llmConfig, currentConfig.EnableDebugLog)
	if err != nil {
		return false, errs.NewIncompleteOrIncorrectSubmissionError(err)
	}
	if err = testTextRecognitionLLM(c, c.GetCurrentUid(), llmConfig, provider); err != nil {
		return false, errs.Or(err, errs.ErrOperationFailed)
	}
	return true, nil
}

type llmSettingsTestResponse struct {
	Status string `json:"status"`
}

func testTextRecognitionLLM(c core.Context, uid int64, config *settings.LLMConfig, provider llmprovider.LargeLanguageModelProvider) error {
	response, err := provider.GetJsonResponse(c, uid, config, &data.LargeLanguageModelRequest{
		Stream: false, SystemPrompt: `Return exactly one JSON object: {"status":"ok"}.`,
		UserPrompt: []byte("Connection test"), UserPromptType: data.LARGE_LANGUAGE_MODEL_REQUEST_PROMPT_TYPE_TEXT,
		ResponseJsonObjectType: reflect.TypeOf(llmSettingsTestResponse{}),
	})
	if err != nil {
		return err
	}
	if response == nil {
		return fmt.Errorf("LLM returned an empty response")
	}
	result := llmSettingsTestResponse{}
	if err = json.Unmarshal([]byte(response.Content), &result); err != nil {
		return fmt.Errorf("invalid LLM test response: %w", err)
	}
	if result.Status != "ok" {
		return fmt.Errorf("unexpected LLM test status %q", result.Status)
	}
	return nil
}

func buildTextRecognitionLLMConfig(request *models.LLMSettingsUpdateRequest, current *settings.LLMConfig) (*settings.LLMConfig, error) {
	config := &settings.LLMConfig{}
	if current != nil {
		*config = *current
	}
	provider := strings.TrimSpace(request.Provider)
	modelID := strings.TrimSpace(request.ModelID)
	endpoint := strings.TrimRight(strings.TrimSpace(request.Endpoint), "/")
	apiKey := strings.TrimSpace(request.APIKey)
	thinking := settings.LLMThinkingLevel(strings.TrimSpace(request.Thinking))
	if !validLLMProvider(provider) {
		return nil, fmt.Errorf("unsupported LLM provider %q", provider)
	}
	if !validLLMThinkingLevel(thinking) {
		return nil, fmt.Errorf("unsupported thinking level %q", thinking)
	}
	if provider != "" && modelID == "" {
		return nil, fmt.Errorf("model id is required")
	}
	if providerRequiresEndpoint(provider) {
		if err := validateLLMEndpoint(endpoint); err != nil {
			return nil, err
		}
	}

	config.LLMProvider = provider
	config.EnableThinking = thinking
	config.LargeLanguageModelAPIRequestTimeout = request.RequestTimeout
	if config.LargeLanguageModelAPIRequestTimeout == 0 {
		config.LargeLanguageModelAPIRequestTimeout = defaultLLMRequestTimeout
	}
	config.LargeLanguageModelAPIProxy = strings.TrimSpace(request.Proxy)
	if config.LargeLanguageModelAPIProxy == "" {
		config.LargeLanguageModelAPIProxy = "system"
	}
	config.LargeLanguageModelAPISkipTLSVerify = request.SkipTLSVerify

	switch provider {
	case settings.OpenAILLMProvider:
		config.OpenAIModelID = modelID
		if apiKey != "" {
			config.OpenAIAPIKey = apiKey
		}
		if config.OpenAIAPIKey == "" {
			return nil, fmt.Errorf("API key is required")
		}
	case settings.OpenAICompatibleLLMProvider, settings.OpenAIResponsesCompatibleLLMProvider:
		config.OpenAICompatibleBaseURL, config.OpenAICompatibleModelID = endpoint, modelID
		if apiKey != "" {
			config.OpenAICompatibleAPIKey = apiKey
		}
		if config.OpenAICompatibleAPIKey == "" {
			return nil, fmt.Errorf("API key is required")
		}
	case settings.AnthropicLLMProvider:
		config.AnthropicModelID = modelID
		if apiKey != "" {
			config.AnthropicAPIKey = apiKey
		}
		if config.AnthropicAPIKey == "" {
			return nil, fmt.Errorf("API key is required")
		}
		ensureAnthropicDefaults(config)
	case settings.AnthropicCompatibleLLMProvider:
		config.AnthropicCompatibleBaseURL, config.AnthropicCompatibleModelID = endpoint, modelID
		if apiKey != "" {
			config.AnthropicCompatibleAPIKey = apiKey
		}
		if config.AnthropicCompatibleAPIKey == "" {
			return nil, fmt.Errorf("API key is required")
		}
		ensureAnthropicDefaults(config)
	case settings.OpenRouterLLMProvider:
		config.OpenRouterModelID = modelID
		if apiKey != "" {
			config.OpenRouterAPIKey = apiKey
		}
		if config.OpenRouterAPIKey == "" {
			return nil, fmt.Errorf("API key is required")
		}
	case settings.OllamaLLMProvider:
		config.OllamaServerURL, config.OllamaModelID = endpoint, modelID
	case settings.LMStudioLLMProvider:
		config.LMStudioServerURL, config.LMStudioModelID = endpoint, modelID
		if apiKey != "" {
			config.LMStudioToken = apiKey
		}
	case settings.GoogleAILLMProvider:
		config.GoogleAIModelID = modelID
		if apiKey != "" {
			config.GoogleAIAPIKey = apiKey
		}
		if config.GoogleAIAPIKey == "" {
			return nil, fmt.Errorf("API key is required")
		}
	}
	return config, nil
}

func textRecognitionLLMSettingsResponse(config *settings.LLMConfig) *models.LLMSettingsResponse {
	if config == nil {
		return &models.LLMSettingsResponse{RequestTimeout: defaultLLMRequestTimeout, Proxy: "system"}
	}
	response := &models.LLMSettingsResponse{
		Provider: config.LLMProvider, Thinking: string(config.EnableThinking),
		RequestTimeout: config.LargeLanguageModelAPIRequestTimeout, Proxy: config.LargeLanguageModelAPIProxy,
		SkipTLSVerify: config.LargeLanguageModelAPISkipTLSVerify,
	}
	if response.RequestTimeout == 0 {
		response.RequestTimeout = defaultLLMRequestTimeout
	}
	if response.Proxy == "" {
		response.Proxy = "system"
	}
	switch config.LLMProvider {
	case settings.OpenAILLMProvider:
		response.ModelID, response.APIKeyConfigured = config.OpenAIModelID, config.OpenAIAPIKey != ""
	case settings.OpenAICompatibleLLMProvider, settings.OpenAIResponsesCompatibleLLMProvider:
		response.Endpoint, response.ModelID = config.OpenAICompatibleBaseURL, config.OpenAICompatibleModelID
		response.APIKeyConfigured = config.OpenAICompatibleAPIKey != ""
	case settings.AnthropicLLMProvider:
		response.ModelID, response.APIKeyConfigured = config.AnthropicModelID, config.AnthropicAPIKey != ""
	case settings.AnthropicCompatibleLLMProvider:
		response.Endpoint, response.ModelID = config.AnthropicCompatibleBaseURL, config.AnthropicCompatibleModelID
		response.APIKeyConfigured = config.AnthropicCompatibleAPIKey != ""
	case settings.OpenRouterLLMProvider:
		response.ModelID, response.APIKeyConfigured = config.OpenRouterModelID, config.OpenRouterAPIKey != ""
	case settings.OllamaLLMProvider:
		response.Endpoint, response.ModelID = config.OllamaServerURL, config.OllamaModelID
	case settings.LMStudioLLMProvider:
		response.Endpoint, response.ModelID = config.LMStudioServerURL, config.LMStudioModelID
		response.APIKeyConfigured = config.LMStudioToken != ""
	case settings.GoogleAILLMProvider:
		response.ModelID, response.APIKeyConfigured = config.GoogleAIModelID, config.GoogleAIAPIKey != ""
	}
	return response
}

func validLLMProvider(provider string) bool {
	switch provider {
	case "", settings.OpenAILLMProvider, settings.OpenAICompatibleLLMProvider,
		settings.OpenAIResponsesCompatibleLLMProvider, settings.AnthropicLLMProvider,
		settings.AnthropicCompatibleLLMProvider, settings.OpenRouterLLMProvider,
		settings.OllamaLLMProvider, settings.LMStudioLLMProvider, settings.GoogleAILLMProvider:
		return true
	default:
		return false
	}
}

func validLLMThinkingLevel(level settings.LLMThinkingLevel) bool {
	switch level {
	case settings.LLMThinkingDefault, settings.LLMThinkingDisabled, settings.LLMThinkingEnabled,
		settings.LLMThinkingLow, settings.LLMThinkingMedium, settings.LLMThinkingHigh, settings.LLMThinkingXHigh:
		return true
	default:
		return false
	}
}

func providerRequiresEndpoint(provider string) bool {
	return provider == settings.OpenAICompatibleLLMProvider || provider == settings.OpenAIResponsesCompatibleLLMProvider ||
		provider == settings.AnthropicCompatibleLLMProvider || provider == settings.OllamaLLMProvider || provider == settings.LMStudioLLMProvider
}

func validateLLMEndpoint(endpoint string) error {
	parsed, err := url.ParseRequestURI(endpoint)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return fmt.Errorf("a valid HTTP(S) endpoint is required")
	}
	return nil
}

func ensureAnthropicDefaults(config *settings.LLMConfig) {
	if config.AnthropicMaxTokens == 0 {
		config.AnthropicMaxTokens = 1024
	}
	if config.AnthropicThinkingBudgetTokens == 0 {
		config.AnthropicThinkingBudgetTokens = 1024
	}
	if config.AnthropicCompatibleMaxTokens == 0 {
		config.AnthropicCompatibleMaxTokens = 1024
	}
	if config.AnthropicCompatibleThinkingBudgetTokens == 0 {
		config.AnthropicCompatibleThinkingBudgetTokens = 1024
	}
	if config.AnthropicCompatibleAPIVersion == "" {
		config.AnthropicCompatibleAPIVersion = "2023-06-01"
	}
}
