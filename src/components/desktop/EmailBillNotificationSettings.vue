<template>
    <section class="notification-settings" :aria-label="tt('Email Notifications')">
        <div class="d-flex align-center justify-space-between flex-wrap ga-3 mb-2">
            <div><h2 class="text-h6">{{ tt('Email Notifications') }}</h2><p class="text-body-2 text-medium-emphasis mt-1">{{ tt('Receive the result of email bookkeeping in your inbox.') }}</p></div>
            <v-switch v-model="notification.enabled" :label="tt('Enable Email Notifications')" color="primary" hide-details />
        </div>
        <v-row class="mt-2">
            <v-col cols="12" md="7"><v-text-field v-model.trim="notification.recipient" type="email" autocomplete="email" :label="tt('Recipient Email')" :placeholder="mailUser" :error-messages="recipientError" :hint="tt('Defaults to your connected mailbox.')" persistent-hint /></v-col>
            <v-col cols="12" md="5"><v-select v-model="notification.mode" :label="tt('Notify When')" :items="frequencies" /></v-col>
        </v-row>
        <v-expansion-panels v-model="advancedPanel" variant="accordion" class="mt-2">
            <v-expansion-panel :title="tt('Sending Mailbox')">
                <v-expansion-panel-text>
                    <p class="text-body-2 text-medium-emphasis mb-4">{{ tt('Common mail providers are filled automatically. Change these only if you use a different sending server.') }}</p>
                    <v-row>
                        <v-col cols="12" md="8"><v-text-field v-model.trim="notification.smtpServer" :label="tt('SMTP Server')" placeholder="smtp.qq.com" :error-messages="notification.enabled && !notification.smtpServer ? tt('Enter an SMTP server hostname') : ''" /></v-col>
                        <v-col cols="12" md="4"><v-text-field v-model.number="notification.smtpPort" type="number" min="1" max="65535" :label="tt('SMTP Port')" :hint="tt('465 uses TLS; other ports require STARTTLS.')" persistent-hint :error-messages="portError" /></v-col>
                        <v-col cols="12"><v-checkbox v-model="notification.useMailboxCredentials" :label="tt('Reuse the connected mailbox password')" color="primary" hide-details /></v-col>
                        <v-col cols="12" md="6"><v-text-field v-model.trim="notification.smtpUser" :disabled="notification.useMailboxCredentials" :label="tt('SMTP Username')" :placeholder="mailUser" /></v-col>
                        <v-col cols="12" md="6" v-if="!notification.useMailboxCredentials"><v-text-field v-model="notification.smtpPassword" type="password" autocomplete="new-password" :label="tt('SMTP Password')" :placeholder="notification.passwordConfigured ? tt('Saved; leave blank to keep') : ''" /></v-col>
                        <v-col cols="12" md="6"><v-text-field v-model.trim="notification.fromAddress" type="email" :label="tt('Sender Email')" :placeholder="mailUser" /></v-col>
                        <v-col cols="12" md="6"><v-text-field v-model.trim="notification.fromName" :label="tt('Sender Name')" placeholder="ezBookkeeping" :hint="tt('Defaults to ezBookkeeping.')" maxlength="100" persistent-hint /></v-col>
                    </v-row>
                </v-expansion-panel-text>
            </v-expansion-panel>
        </v-expansion-panels>
        <div class="notification-actions d-flex align-center justify-space-between flex-wrap ga-3 mt-4">
            <div class="text-body-2 text-medium-emphasis">{{ tt('Sender Email') }}: {{ senderLabel }}<span v-if="notification.useMailboxCredentials && passwordConfigured" class="d-block text-caption mt-1">{{ tt('Uses the saved mailbox password.') }}</span></div>
            <div class="d-flex flex-wrap ga-2"><v-btn variant="text" @click="openPreview">{{ tt('Preview Notification Email') }}</v-btn><v-btn variant="tonal" :loading="testing" :disabled="!canTest" @click="sendTest">{{ tt('Send Test Email') }}</v-btn></div>
        </div>
        <v-alert v-if="feedback" class="mt-3" :type="feedbackType" variant="tonal" density="compact" aria-live="polite">{{ feedback }}</v-alert>

        <v-dialog v-model="previewDialog" max-width="760" scrollable>
            <v-card>
                <v-card-title class="d-flex align-center flex-wrap ga-2"><span>{{ tt('Preview Notification Email') }}</span><v-spacer /><v-btn variant="text" @click="previewDialog = false">{{ tt('Close') }}</v-btn></v-card-title>
                <v-card-text class="notification-preview-content">
                    <v-select v-model="previewOutcome" :items="previewOutcomes" :label="tt('Preview Scenario')" density="compact" @update:model-value="loadPreview" />
                    <p class="text-caption text-medium-emphasis mb-3">{{ tt('Preview uses sample data and does not send an email.') }}</p>
                    <v-alert v-if="previewError" class="mb-3" type="error" variant="tonal" density="compact" aria-live="polite">{{ previewError }}</v-alert>
                    <dl v-if="preview" class="notification-envelope mb-4"><div><dt>{{ tt('Sender Email') }}</dt><dd>{{ senderLabel }}</dd></div><div><dt>{{ tt('Recipient Email') }}</dt><dd>{{ notification.recipient || mailUser }}</dd></div><div><dt>{{ tt('Subject') }}</dt><dd>{{ preview.subject }}</dd></div></dl>
                    <v-progress-linear v-if="previewLoading" indeterminate color="primary" class="mb-3" :aria-label="tt('Loading...')" />
                    <iframe v-if="preview" class="notification-preview-frame" :srcdoc="preview.html" sandbox="" :title="tt('Notification Email Preview')" referrerpolicy="no-referrer" />
                </v-card-text>
            </v-card>
        </v-dialog>
    </section>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue';
import { isAxiosError } from 'axios';
import type { EmailBillNotificationSettings, EmailBillNotificationPreview } from '@/core/emailBill.ts';
import { useI18n } from '@/locales/helpers.ts';
import services from '@/lib/services.ts';

const notification = defineModel<EmailBillNotificationSettings>({ required: true });
const props = defineProps<{ mailUser: string; passwordConfigured: boolean }>();
const { tt } = useI18n();
const advancedPanel = ref<number | undefined>();
const testing = ref(false);
const feedback = ref('');
const feedbackType = ref<'success' | 'error'>('success');
const previewDialog = ref(false);
const previewOutcome = ref('succeeded');
const previewLoading = ref(false);
const previewError = ref('');
const preview = ref<EmailBillNotificationPreview | null>(null);
let previewRequest = 0;
const frequencies = computed(() => [
    { title: tt('Every Run'), value: 'always' }, { title: tt('New Bills Only'), value: 'changes_only' }, { title: tt('Errors Only'), value: 'errors_only' }, { title: tt('New Bills or Errors'), value: 'changes_or_errors' }
]);
const previewOutcomes = computed(() => [
    { title: tt('Successful Run'), value: 'succeeded' }, { title: tt('Run with Errors'), value: 'failed' }, { title: tt('No New Bills'), value: 'empty' }
]);
const recipientValid = computed(() => /^[^@\s]+@[^@\s]+\.[^@\s]+$/.test(notification.value.recipient || props.mailUser));
const recipientError = computed(() => notification.value.enabled && !recipientValid.value ? tt('Enter a valid recipient email') : '');
const portValid = computed(() => Number.isInteger(notification.value.smtpPort) && notification.value.smtpPort > 0 && notification.value.smtpPort <= 65535);
const portError = computed(() => !portValid.value ? tt('Enter a port between 1 and 65535') : '');
const canTest = computed(() => recipientValid.value && portValid.value && Boolean(notification.value.smtpServer) && (notification.value.useMailboxCredentials ? props.passwordConfigured : Boolean(notification.value.smtpPassword || notification.value.passwordConfigured)));
const senderLabel = computed(() => {
    const address = notification.value.fromAddress || props.mailUser || tt('Not configured');
    return `${notification.value.fromName?.trim() || 'ezBookkeeping'} <${address}>`;
});

watch(() => notification.value.enabled, enabled => { if (enabled && !notification.value.smtpServer) advancedPanel.value = 0; });
watch(() => [notification.value.useMailboxCredentials, props.mailUser] as const, ([reuse, mailUser]) => { if (reuse) notification.value.smtpUser = mailUser; });

function errorText(error: unknown): string {
    const message = isAxiosError<{ errorMessage?: string }>(error) ? error.response?.data?.errorMessage : undefined;
    return tt(message || (error instanceof Error ? error.message : 'Email bill request failed'));
}
async function sendTest(): Promise<void> {
    testing.value = true; feedback.value = '';
    try {
        const response = await services.testEmailBillNotification({ ...notification.value });
        if (!response.data.success) throw new Error('Email bill request failed');
        if (!response.data.result.sent) throw new Error(response.data.result.error || 'Email bill request failed');
        feedbackType.value = 'success'; feedback.value = tt('Test email sent. Check your inbox.');
    } catch (error) { feedbackType.value = 'error'; feedback.value = errorText(error); }
    finally { testing.value = false; }
}
async function openPreview(): Promise<void> { previewDialog.value = true; await loadPreview(); }
async function loadPreview(): Promise<void> {
    const request = ++previewRequest;
    previewLoading.value = true; previewError.value = ''; preview.value = null;
    try {
        const response = await services.previewEmailBillNotification(previewOutcome.value);
        if (!response.data.success) throw new Error('Email bill request failed');
        if (request === previewRequest) preview.value = response.data.result;
    } catch (error) { if (request === previewRequest) previewError.value = errorText(error); }
    finally { if (request === previewRequest) previewLoading.value = false; }
}
</script>

<style scoped>
.notification-settings { border-block-start: thin solid rgba(var(--v-border-color), var(--v-border-opacity)); padding-block-start: 24px; margin-block-start: 24px; }
.notification-preview-content { padding-block-end: 0; }
.notification-envelope { font-size: 0.8rem; line-height: 1.6; }
.notification-envelope > div { display: grid; grid-template-columns: 110px minmax(0, 1fr); gap: 12px; margin-block: 4px; }
.notification-envelope dt { color: rgba(var(--v-theme-on-surface), 0.65); }
.notification-envelope dd { margin: 0; overflow-wrap: anywhere; }
.notification-preview-frame { display: block; width: 100%; height: min(640px, 65vh); border: 0; background: #f3f5f8; }
@media (max-width: 600px) { .notification-envelope > div { grid-template-columns: 80px minmax(0, 1fr); } .notification-actions > div { width: 100%; } }
</style>
