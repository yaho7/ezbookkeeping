package llm

import (
	"github.com/mayswind/ezbookkeeping/pkg/core"
	"github.com/mayswind/ezbookkeeping/pkg/errs"
	"github.com/mayswind/ezbookkeeping/pkg/llm/data"
	"github.com/mayswind/ezbookkeeping/pkg/llm/provider"
	"github.com/mayswind/ezbookkeeping/pkg/llm/provider/anthropic"
	"github.com/mayswind/ezbookkeeping/pkg/llm/provider/googleai"
	"github.com/mayswind/ezbookkeeping/pkg/llm/provider/lmstudio"
	"github.com/mayswind/ezbookkeeping/pkg/llm/provider/ollama"
	"github.com/mayswind/ezbookkeeping/pkg/llm/provider/openai"
	"github.com/mayswind/ezbookkeeping/pkg/settings"
)

// LargeLanguageModelProviderContainer contains the current large language model provider
type LargeLanguageModelProviderContainer struct{}

// Initialize a large language model provider container singleton instance
var (
	Container = &LargeLanguageModelProviderContainer{}
)

// InitializeLargeLanguageModelProvider initializes the current large language model provider according to the config
func InitializeLargeLanguageModelProvider(config *settings.Config) error {
	for _, candidate := range []*settings.LLMConfig{config.TextRecognitionLLMConfig, config.ReceiptImageRecognitionLLMConfig} {
		if candidate != nil {
			if _, err := initializeLargeLanguageModelProvider(candidate, config.EnableDebugLog); err != nil {
				return err
			}
		}
	}
	return nil
}

func initializeLargeLanguageModelProvider(llmConfig *settings.LLMConfig, enableResponseLog bool) (provider.LargeLanguageModelProvider, error) {
	if llmConfig.LLMProvider == settings.OpenAILLMProvider {
		return openai.NewOpenAILargeLanguageModelProvider(llmConfig, enableResponseLog), nil
	} else if llmConfig.LLMProvider == settings.OpenAICompatibleLLMProvider {
		return openai.NewOpenAICompatibleLargeLanguageModelProvider(llmConfig, enableResponseLog), nil
	} else if llmConfig.LLMProvider == settings.OpenAIResponsesCompatibleLLMProvider {
		return openai.NewOpenAIResponsesCompatibleLargeLanguageModelProvider(llmConfig, enableResponseLog), nil
	} else if llmConfig.LLMProvider == settings.AnthropicLLMProvider {
		return anthropic.NewAnthropicLargeLanguageModelProvider(llmConfig, enableResponseLog), nil
	} else if llmConfig.LLMProvider == settings.AnthropicCompatibleLLMProvider {
		return anthropic.NewAnthropicCompatibleLargeLanguageModelProvider(llmConfig, enableResponseLog), nil
	} else if llmConfig.LLMProvider == settings.OpenRouterLLMProvider {
		return openai.NewOpenRouterLargeLanguageModelProvider(llmConfig, enableResponseLog), nil
	} else if llmConfig.LLMProvider == settings.OllamaLLMProvider {
		return ollama.NewOllamaLargeLanguageModelProvider(llmConfig, enableResponseLog), nil
	} else if llmConfig.LLMProvider == settings.LMStudioLLMProvider {
		return lmstudio.NewLMStudioLargeLanguageModelProvider(llmConfig, enableResponseLog), nil
	} else if llmConfig.LLMProvider == settings.GoogleAILLMProvider {
		return googleai.NewGoogleAILargeLanguageModelProvider(llmConfig, enableResponseLog), nil
	} else if llmConfig.LLMProvider == "" {
		return nil, nil
	}

	return nil, errs.ErrInvalidLLMProvider
}

// NewLargeLanguageModelProvider creates an isolated provider without changing the global container.
func NewLargeLanguageModelProvider(llmConfig *settings.LLMConfig, enableResponseLog bool) (provider.LargeLanguageModelProvider, error) {
	return initializeLargeLanguageModelProvider(llmConfig, enableResponseLog)
}

// GetJsonResponseByTextRecognitionModel uses one immutable configuration snapshot for both provider and request.
func (l *LargeLanguageModelProviderContainer) GetJsonResponseByTextRecognitionModel(c core.Context, uid int64, config *settings.Config, request *data.LargeLanguageModelRequest) (*data.LargeLanguageModelTextualResponse, error) {
	if config == nil {
		return nil, errs.ErrInvalidLLMProvider
	}
	return getJsonResponse(c, uid, config.TextRecognitionLLMConfig, config.EnableDebugLog, request)
}

// GetJsonResponseByReceiptImageRecognitionModel uses the request's receipt configuration.
func (l *LargeLanguageModelProviderContainer) GetJsonResponseByReceiptImageRecognitionModel(c core.Context, uid int64, config *settings.Config, request *data.LargeLanguageModelRequest) (*data.LargeLanguageModelTextualResponse, error) {
	if config == nil {
		return nil, errs.ErrInvalidLLMProvider
	}
	return getJsonResponse(c, uid, config.ReceiptImageRecognitionLLMConfig, config.EnableDebugLog, request)
}

func getJsonResponse(c core.Context, uid int64, config *settings.LLMConfig, debug bool, request *data.LargeLanguageModelRequest) (*data.LargeLanguageModelTextualResponse, error) {
	if config == nil || config.LLMProvider == "" {
		return nil, errs.ErrInvalidLLMProvider
	}
	adapter, err := initializeLargeLanguageModelProvider(config, debug)
	if err != nil {
		return nil, err
	}
	return adapter.GetJsonResponse(c, uid, config, request)
}
