package models

// LLMSettingsResponse is the redacted application-level text LLM configuration.
type LLMSettingsResponse struct {
	CanManage         bool   `json:"canManage"`
	ManagedExternally bool   `json:"managedExternally"`
	Provider          string `json:"provider"`
	Endpoint          string `json:"endpoint"`
	ModelID           string `json:"modelId"`
	APIKeyConfigured  bool   `json:"apiKeyConfigured"`
	Thinking          string `json:"thinking"`
	RequestTimeout    uint32 `json:"requestTimeout"`
	Proxy             string `json:"proxy"`
	SkipTLSVerify     bool   `json:"skipTlsVerify"`
}

// LLMSettingsUpdateRequest contains editable application-level text LLM settings.
type LLMSettingsUpdateRequest struct {
	ClearAPIKey    bool   `json:"clearApiKey"`
	Provider       string `json:"provider"`
	Endpoint       string `json:"endpoint"`
	ModelID        string `json:"modelId"`
	APIKey         string `json:"apiKey"`
	Thinking       string `json:"thinking"`
	RequestTimeout uint32 `json:"requestTimeout"`
	Proxy          string `json:"proxy"`
	SkipTLSVerify  bool   `json:"skipTlsVerify"`
}
