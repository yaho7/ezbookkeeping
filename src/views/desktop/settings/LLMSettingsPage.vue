<template>
    <v-row>
        <v-col cols="12">
            <v-card>
                <v-card-title class="d-flex align-center flex-wrap ga-3">
                    <span>{{ tt('AI Settings') }}</span>
                    <v-chip :color="settings.provider ? 'success' : undefined" size="small" variant="tonal">
                        {{ settings.provider ? tt('Enabled') : tt('Disabled') }}
                    </v-chip>
                </v-card-title>
                <v-card-subtitle class="pb-3 text-wrap">
                    {{ tt('Configure the global language model used by AI features. Email bill automation uses this configuration when rules need AI.') }}
                </v-card-subtitle>
                <v-divider />
                <v-form @submit.prevent="save">
                    <v-card-text>
                        <v-alert class="mb-5" type="info" variant="tonal">
                            {{ tt('The API key is never returned to the browser. Leave it blank to keep the saved key.') }}
                        </v-alert>
                        <v-row>
                            <v-col cols="12" md="6">
                                <v-select
                                    :label="tt('Provider')"
                                    :items="providers"
                                    item-title="title"
                                    item-value="value"
                                    v-model="settings.provider"
                                />
                            </v-col>
                            <v-col cols="12" md="6">
                                <v-text-field
                                    :disabled="!settings.provider"
                                    :label="tt('Model ID')"
                                    placeholder="gpt-4.1-mini"
                                    v-model.trim="settings.modelId"
                                />
                            </v-col>
                            <v-col v-if="requiresEndpoint" cols="12">
                                <v-text-field
                                    :label="tt('API Endpoint')"
                                    :placeholder="endpointPlaceholder"
                                    v-model.trim="settings.endpoint"
                                />
                            </v-col>
                            <v-col v-if="showsAPIKey" cols="12" md="6">
                                <v-text-field
                                    type="password"
                                    autocomplete="new-password"
                                    :label="tt('API Key')"
                                    :placeholder="settings.apiKeyConfigured ? tt('Saved; leave blank to keep') : ''"
                                    v-model="settings.apiKey"
                                />
                            </v-col>
                            <v-col cols="12" md="6">
                                <v-select
                                    :disabled="!settings.provider"
                                    :label="tt('Thinking Level')"
                                    :items="thinkingLevels"
                                    item-title="title"
                                    item-value="value"
                                    v-model="settings.thinking"
                                />
                            </v-col>
                            <v-col cols="12" md="4">
                                <v-text-field
                                    type="number"
                                    min="1000"
                                    :disabled="!settings.provider"
                                    :label="tt('Request Timeout (ms)')"
                                    v-model.number="settings.requestTimeout"
                                />
                            </v-col>
                            <v-col cols="12" md="5">
                                <v-text-field
                                    :disabled="!settings.provider"
                                    :label="tt('Proxy')"
                                    hint="system, direct, http://127.0.0.1:7890"
                                    persistent-hint
                                    v-model.trim="settings.proxy"
                                />
                            </v-col>
                            <v-col cols="12" md="3">
                                <v-switch
                                    color="primary"
                                    :disabled="!settings.provider"
                                    :label="tt('Skip TLS Verification')"
                                    v-model="settings.skipTlsVerify"
                                />
                            </v-col>
                        </v-row>
                    </v-card-text>
                    <v-card-actions class="px-6 pb-5">
                        <v-btn variant="text" :loading="loading" @click="load">{{ tt('Refresh') }}</v-btn>
                        <v-spacer />
                        <v-btn variant="tonal" :disabled="!settings.provider" :loading="testing" @click="testSettings">{{ tt('Test Connection') }}</v-btn>
                        <v-btn color="primary" type="submit" :loading="saving">{{ tt('Save') }}</v-btn>
                    </v-card-actions>
                </v-form>
            </v-card>
        </v-col>
    </v-row>

    <snack-bar ref="snackbar" />
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref, useTemplateRef } from 'vue';

import SnackBar from '@/components/desktop/SnackBar.vue';
import { createLLMSettings, type LLMSettings } from '@/core/llm.ts';
import { useI18n } from '@/locales/helpers.ts';
import services from '@/lib/services.ts';

type SnackBarType = InstanceType<typeof SnackBar>;

const { tt } = useI18n();
const snackbar = useTemplateRef<SnackBarType>('snackbar');
const settings = reactive<LLMSettings>(createLLMSettings());
const loading = ref(false);
const saving = ref(false);
const testing = ref(false);

const providers = computed(() => [
    { title: tt('Disabled'), value: '' },
    { title: 'OpenAI', value: 'openai' },
    { title: tt('OpenAI Compatible'), value: 'openai_compatible' },
    { title: tt('OpenAI Responses Compatible'), value: 'openai_responses_compatible' },
    { title: 'Anthropic', value: 'anthropic' },
    { title: tt('Anthropic Compatible'), value: 'anthropic_compatible' },
    { title: 'OpenRouter', value: 'openrouter' },
    { title: 'Ollama', value: 'ollama' },
    { title: 'LM Studio', value: 'lm_studio' },
    { title: 'Google AI', value: 'google_ai' }
]);
const thinkingLevels = computed(() => [
    { title: tt('Provider Default'), value: '' },
    { title: tt('Off'), value: 'off' },
    { title: tt('On'), value: 'on' },
    { title: tt('Low'), value: 'low' },
    { title: tt('Medium'), value: 'medium' },
    { title: tt('High'), value: 'high' },
    { title: tt('Extra High'), value: 'xhigh' }
]);
const requiresEndpoint = computed(() => ['openai_compatible', 'openai_responses_compatible', 'anthropic_compatible', 'ollama', 'lm_studio'].includes(settings.provider));
const showsAPIKey = computed(() => settings.provider !== '' && settings.provider !== 'ollama');
const endpointPlaceholder = computed(() => settings.provider === 'ollama' ? 'http://ollama:11434' : settings.provider === 'lm_studio' ? 'http://127.0.0.1:1234' : 'https://api.example.com/v1');

onMounted(load);

async function load(): Promise<void> {
    loading.value = true;
    try {
        const response = await services.getLLMSettings();
        Object.assign(settings, resultOf(response), { apiKey: '' });
    } catch (error) {
        showError(error);
    } finally {
        loading.value = false;
    }
}

async function save(): Promise<void> {
    saving.value = true;
    try {
        const response = await services.updateLLMSettings({ ...settings });
        Object.assign(settings, resultOf(response), { apiKey: '' });
        snackbar.value?.showMessage('Data has been updated');
    } catch (error) {
        showError(error);
    } finally {
        saving.value = false;
    }
}

async function testSettings(): Promise<void> {
    testing.value = true;
    try {
        resultOf(await services.testLLMSettings({ ...settings }));
        snackbar.value?.showMessage('Connection test succeeded');
    } catch (error) {
        showError(error);
    } finally {
        testing.value = false;
    }
}

function resultOf<T>(response: { data: { success: boolean; result: T } }): T {
    if (!response.data?.success) throw new Error('AI settings request failed');
    return response.data.result;
}

function showError(error: unknown): void {
    snackbar.value?.showError(error instanceof Error ? error : String(error));
}
</script>
