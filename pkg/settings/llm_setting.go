package settings

import (
	"strconv"

	"gopkg.in/ini.v1"
)

// SaveTextRecognitionLLMConfiguration persists the application text LLM section.
func SaveTextRecognitionLLMConfiguration(configFilePath string, config *LLMConfig) error {
	return updateConfigurationFile(configFilePath, func(configFile *ini.File) error {

		section, err := configFile.NewSection("llm_text_recognition")
		if err != nil {
			return err
		}
		values := map[string]string{
			"llm_provider": config.LLMProvider, "enable_thinking": string(config.EnableThinking),
			"openai_api_key": config.OpenAIAPIKey, "openai_model_id": config.OpenAIModelID,
			"openai_compatible_base_url": config.OpenAICompatibleBaseURL, "openai_compatible_api_key": config.OpenAICompatibleAPIKey,
			"openai_compatible_model_id": config.OpenAICompatibleModelID,
			"anthropic_api_key":          config.AnthropicAPIKey, "anthropic_model_id": config.AnthropicModelID,
			"anthropic_max_tokens":             strconv.FormatUint(uint64(config.AnthropicMaxTokens), 10),
			"anthropic_thinking_budget_tokens": strconv.FormatUint(uint64(config.AnthropicThinkingBudgetTokens), 10),
			"anthropic_compatible_base_url":    config.AnthropicCompatibleBaseURL, "anthropic_compatible_api_version": config.AnthropicCompatibleAPIVersion,
			"anthropic_compatible_api_key": config.AnthropicCompatibleAPIKey, "anthropic_compatible_model_id": config.AnthropicCompatibleModelID,
			"anthropic_compatible_max_tokens":             strconv.FormatUint(uint64(config.AnthropicCompatibleMaxTokens), 10),
			"anthropic_compatible_thinking_budget_tokens": strconv.FormatUint(uint64(config.AnthropicCompatibleThinkingBudgetTokens), 10),
			"openrouter_api_key":                          config.OpenRouterAPIKey, "openrouter_model_id": config.OpenRouterModelID,
			"ollama_server_url": config.OllamaServerURL, "ollama_model_id": config.OllamaModelID,
			"lm_studio_server_url": config.LMStudioServerURL, "lm_studio_token": config.LMStudioToken, "lm_studio_model_id": config.LMStudioModelID,
			"google_ai_api_key": config.GoogleAIAPIKey, "google_ai_model_id": config.GoogleAIModelID,
			"request_timeout": strconv.FormatUint(uint64(config.LargeLanguageModelAPIRequestTimeout), 10),
			"proxy":           config.LargeLanguageModelAPIProxy, "skip_tls_verify": strconv.FormatBool(config.LargeLanguageModelAPISkipTLSVerify),
		}
		for key, value := range values {
			section.Key(key).SetValue(value)
		}
		return nil
	})
}
