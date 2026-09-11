export interface LLMSettings {
    canManage: boolean;
    managedExternally: boolean;
    clearApiKey?: boolean;
    provider: string;
    endpoint: string;
    modelId: string;
    apiKeyConfigured: boolean;
    apiKey?: string;
    thinking: string;
    requestTimeout: number;
    proxy: string;
    skipTlsVerify: boolean;
}

export function createLLMSettings(): LLMSettings {
    return {
        canManage: false,
        managedExternally: false,
        clearApiKey: false,
        provider: '',
        endpoint: '',
        modelId: '',
        apiKeyConfigured: false,
        apiKey: '',
        thinking: '',
        requestTimeout: 60000,
        proxy: 'system',
        skipTlsVerify: false
    };
}
