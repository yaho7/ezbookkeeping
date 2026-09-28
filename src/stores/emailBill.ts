import { ref } from 'vue';
import { defineStore } from 'pinia';

import type {
    EmailBillSettings, EmailBillParserRule, EmailBillMessageSample,
    EmailBillParserPreview, EmailBillRoutingRule, EmailBillClassificationRule,
    EmailBillGeneratedParser
} from '@/core/emailBill.ts';
import services from '@/lib/services.ts';

function resultOf<T>(response: { data: { success: boolean; result: T } }): T {
    if (!response.data?.success) {
        throw new Error('Email bill request failed');
    }
    return response.data.result;
}

export const useEmailBillStore = defineStore('emailBill', () => {
    const settings = ref<EmailBillSettings | null>(null);
    const parsers = ref<EmailBillParserRule[]>([]);
    const messages = ref<EmailBillMessageSample[]>([]);
    const routes = ref<EmailBillRoutingRule[]>([]);
    const classifications = ref<EmailBillClassificationRule[]>([]);

    async function loadAll(): Promise<void> {
        const responses = await Promise.all([
            services.getEmailBillSettings(), services.listEmailBillParsers(), services.listEmailBillMessages(),
            services.listEmailBillRoutes(), services.listEmailBillClassifications()
        ]);
        settings.value = resultOf(responses[0]);
        parsers.value = resultOf(responses[1]);
        messages.value = resultOf(responses[2]);
        routes.value = resultOf(responses[3]);
        classifications.value = resultOf(responses[4]);
    }

    async function saveSettings(value: EmailBillSettings): Promise<void> {
        settings.value = resultOf(await services.updateEmailBillSettings(value));
    }

    async function saveParser(value: Partial<EmailBillParserRule>): Promise<void> {
        const saved = resultOf(await services.saveEmailBillParser(value));
        parsers.value = [...parsers.value.filter(item => item.id !== saved.id), saved];
    }

    async function testParser(request: Record<string, unknown>): Promise<EmailBillParserPreview> {
        return resultOf(await services.testEmailBillParser(request));
    }

    async function generateParser(request: Record<string, unknown>): Promise<EmailBillGeneratedParser> {
        return resultOf(await services.generateEmailBillParser(request));
    }

    async function disableParser(id: string): Promise<void> {
        resultOf(await services.disableEmailBillParser(id));
        const rule = parsers.value.find(item => item.id === id);
        if (rule) rule.enabled = false;
    }

    async function saveRoute(value: Partial<EmailBillRoutingRule>): Promise<void> {
        await services.saveEmailBillRoute(value);
        routes.value = resultOf(await services.listEmailBillRoutes());
    }

    async function disableRoute(id: string): Promise<void> {
        resultOf(await services.disableEmailBillRoute(id));
        routes.value = resultOf(await services.listEmailBillRoutes());
    }

    async function saveClassification(value: Partial<EmailBillClassificationRule>): Promise<void> {
        await services.saveEmailBillClassification(value);
        classifications.value = resultOf(await services.listEmailBillClassifications());
    }

    async function disableClassification(id: string): Promise<void> {
        resultOf(await services.disableEmailBillClassification(id));
        classifications.value = resultOf(await services.listEmailBillClassifications());
    }

    async function deleteClassification(id: string): Promise<void> {
        resultOf(await services.deleteEmailBillClassification(id));
        classifications.value = classifications.value.filter(item => item.id !== id);
    }

    return {
        settings, parsers, messages, routes, classifications,
        loadAll, saveSettings, saveParser, testParser, generateParser, disableParser,
        saveRoute, disableRoute, saveClassification, disableClassification, deleteClassification
    };
});
