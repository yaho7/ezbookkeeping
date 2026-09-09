<template>
    <v-row>
        <v-col cols="12">
            <v-card>
                <v-card-title class="d-flex align-center flex-wrap ga-3">
                    <span>{{ tt('Email Bill Automation') }}</span>
                    <v-chip :color="settings.enabled ? 'success' : undefined" size="small" variant="tonal">
                        {{ settings.enabled ? tt('Enabled') : tt('Disabled') }}
                    </v-chip>
                    <v-spacer />
                    <v-btn prepend-icon="$refresh" variant="text" :loading="loading" @click="loadPage">{{ tt('Refresh') }}</v-btn>
                    <v-btn color="primary" prepend-icon="$play" :loading="running" @click="runNow">{{ tt('Run Now') }}</v-btn>
                </v-card-title>
                <v-card-subtitle class="pb-3 text-wrap">
                    {{ tt('Import authenticated bank emails through independent parsers, routing rules and auditable classification decisions.') }}
                </v-card-subtitle>

                <v-tabs v-model="activeTab" color="primary" show-arrows>
                    <v-tab value="mailbox">{{ tt('Mailbox & Schedule') }}</v-tab>
                    <v-tab value="parsers">{{ tt('Parser Rules') }}</v-tab>
                    <v-tab value="routing">{{ tt('Account Routing') }}</v-tab>
                    <v-tab value="classification">{{ tt('Classification Rules') }}</v-tab>
                    <v-tab value="review">{{ tt('Review & Audit') }}</v-tab>
                </v-tabs>
            </v-card>
        </v-col>

        <v-col cols="12">
            <v-window v-model="activeTab">
                <v-window-item value="mailbox">
                    <v-card :title="tt('Mailbox & Schedule')">
                        <v-card-text>
                            <v-alert class="mb-5" type="info" variant="tonal">
                                {{ tt('The password is never returned to the browser. Leave it blank to keep the saved password.') }}
                            </v-alert>
                            <v-row>
                                <v-col cols="12" md="4"><v-switch color="primary" :label="tt('Enable Email Bill Automation')" v-model="settings.enabled" /></v-col>
                                <v-col cols="12" md="8"><v-text-field :label="tt('IMAP Server')" v-model.trim="settings.imapServer" /></v-col>
                                <v-col cols="12" md="4"><v-text-field type="number" :label="tt('IMAP Port')" v-model.number="settings.imapPort" /></v-col>
                                <v-col cols="12" md="4"><v-text-field :label="tt('Mailbox User')" v-model.trim="settings.mailUser" /></v-col>
                                <v-col cols="12" md="4">
                                    <v-text-field type="password" :label="tt('Mailbox Password')" :placeholder="settings.passwordConfigured ? tt('Saved; leave blank to keep') : ''" v-model="mailPassword" />
                                </v-col>
                                <v-col cols="12" md="4"><v-select :label="tt('Schedule Type')" :items="scheduleModes" item-title="title" item-value="value" v-model="scheduleMode" /></v-col>
                                <v-col cols="12" md="4" v-if="scheduleMode !== 'advanced'"><v-text-field type="time" :label="tt('Run Time')" v-model="scheduleTime" /></v-col>
                                <v-col cols="12" md="4" v-if="scheduleMode === 'weekly'"><v-select :label="tt('Weekday')" :items="weekdays" item-title="title" item-value="value" v-model="scheduleWeekday" /></v-col>
                                <v-col cols="12" md="8" v-if="scheduleMode === 'advanced'">
                                    <v-text-field :label="tt('Cron Expression')" hint="minute hour day month weekday" persistent-hint v-model.trim="settings.cronExpression" />
                                </v-col>
                                <v-col cols="12" md="4"><v-text-field :label="tt('Timezone')" placeholder="Asia/Shanghai" v-model.trim="settings.timezone" /></v-col>
                                <v-col cols="12" md="4"><v-text-field type="number" min="1" max="200" :label="tt('Maximum Emails Per Run')" v-model.number="settings.maxEmails" /></v-col>
                                <v-col cols="12" md="4"><v-switch color="primary" :label="tt('Require Authentication Results')" v-model="settings.requireAuthenticationResults" /></v-col>
                                <v-col cols="12"><v-combobox multiple chips closable-chips :label="tt('Trusted Authentication Domains')" v-model="settings.trustedAuthservDomains" /></v-col>
                                <v-col cols="12" md="4"><v-switch color="primary" :label="tt('Retain Raw Emails for Test Bench')" v-model="settings.retainRawEmails" /></v-col>
                                <v-col cols="12" md="4"><v-text-field type="number" min="1" max="365" :disabled="!settings.retainRawEmails" :label="tt('Raw Email Retention Days')" v-model.number="settings.rawEmailRetentionDays" /></v-col>
                            </v-row>
                        </v-card-text>
                        <v-card-actions class="px-6 pb-5"><v-spacer /><v-btn color="primary" :loading="saving" @click="saveSettings">{{ tt('Save') }}</v-btn></v-card-actions>
                    </v-card>
                </v-window-item>

                <v-window-item value="parsers">
                    <v-card>
                        <v-card-title class="d-flex align-center"><span>{{ tt('Independent Parser Rules') }}</span><v-spacer /><v-btn color="primary" prepend-icon="$plus" @click="openParser()">{{ tt('Add') }}</v-btn></v-card-title>
                        <v-card-subtitle class="pb-3 text-wrap">{{ tt('Every matching rule runs in its own sandbox. A failed rule does not stop the others.') }}</v-card-subtitle>
                        <v-table>
                            <thead><tr><th>{{ tt('Name') }}</th><th>{{ tt('Bank') }}</th><th>{{ tt('Priority') }}</th><th>{{ tt('Status') }}</th><th class="text-right">{{ tt('Actions') }}</th></tr></thead>
                            <tbody>
                                <tr v-for="rule in store.parsers" :key="rule.id">
                                    <td>{{ rule.name }} <v-chip v-if="rule.createdBy.startsWith('preset:')" class="ml-2" size="x-small" variant="tonal">{{ tt('Preset') }}</v-chip></td><td>{{ rule.bank || '—' }}</td><td>{{ rule.priority }}</td>
                                    <td><v-chip size="small" :color="rule.enabled ? 'success' : undefined" variant="tonal">{{ rule.enabled ? tt('Enabled') : tt('Disabled') }}</v-chip></td>
                                    <td class="text-right"><v-btn size="small" variant="text" @click="openParser(rule)">{{ tt('Edit') }}</v-btn><v-btn v-if="rule.enabled" size="small" color="error" variant="text" @click="disableParser(rule.id)">{{ tt('Disable') }}</v-btn></td>
                                </tr>
                                <tr v-if="!store.parsers.length"><td colspan="5" class="text-center text-medium-emphasis py-8">{{ tt('No parser rules') }}</td></tr>
                            </tbody>
                        </v-table>
                    </v-card>
                </v-window-item>

                <v-window-item value="routing">
                    <v-card>
                        <v-card-title class="d-flex align-center"><span>{{ tt('Account Routing') }}</span><v-spacer /><v-btn color="primary" prepend-icon="$plus" @click="openRoute()">{{ tt('Add') }}</v-btn></v-card-title>
                        <v-card-subtitle class="pb-3 text-wrap">{{ tt('Route by bank, account kind, last four digits, currency or mailbox. Empty fields match any value.') }}</v-card-subtitle>
                        <v-table><thead><tr><th>{{ tt('Conditions') }}</th><th>{{ tt('Target Account') }}</th><th>{{ tt('Priority') }}</th><th>{{ tt('Status') }}</th><th class="text-right">{{ tt('Actions') }}</th></tr></thead>
                            <tbody><tr v-for="rule in store.routes" :key="rule.id"><td>{{ routeSummary(rule) }}</td><td>{{ accountName(rule.targetAccountId) }}</td><td>{{ rule.priority }}</td><td>{{ rule.enabled ? tt('Enabled') : tt('Disabled') }}</td><td class="text-right"><v-btn size="small" variant="text" @click="openRoute(rule)">{{ tt('Edit') }}</v-btn><v-btn v-if="rule.enabled" size="small" color="error" variant="text" @click="disableRoute(rule.id)">{{ tt('Disable') }}</v-btn></td></tr><tr v-if="!store.routes.length"><td colspan="5" class="text-center text-medium-emphasis py-8">{{ tt('No routing rules') }}</td></tr></tbody>
                        </v-table>
                    </v-card>
                </v-window-item>

                <v-window-item value="classification">
                    <v-card>
                        <v-card-title class="d-flex align-center"><span>{{ tt('Classification Rules') }}</span><v-spacer /><v-btn color="primary" prepend-icon="$plus" @click="openClassification()">{{ tt('Add') }}</v-btn></v-card-title>
                        <v-card-subtitle class="pb-3 text-wrap">{{ tt('Rules are matched before LLM classification. Learned mappings remain visible, editable and reversible.') }}</v-card-subtitle>
                        <v-table><thead><tr><th>{{ tt('Merchant Pattern') }}</th><th>{{ tt('Match Type') }}</th><th>{{ tt('Category') }}</th><th>{{ tt('Source') }}</th><th>{{ tt('Status') }}</th><th class="text-right">{{ tt('Actions') }}</th></tr></thead>
                            <tbody><tr v-for="rule in store.classifications" :key="rule.id"><td>{{ rule.merchantPattern }}</td><td>{{ rule.matchType }}</td><td>{{ categoryName(rule.categoryId) }}</td><td><v-chip size="small" variant="tonal">{{ rule.origin }}</v-chip></td><td>{{ rule.enabled ? tt('Enabled') : tt('Disabled') }}</td><td class="text-right"><v-btn size="small" variant="text" @click="openClassification(rule)">{{ tt('Edit') }}</v-btn><v-btn v-if="rule.enabled" size="small" variant="text" @click="disableClassification(rule.id)">{{ tt('Disable') }}</v-btn><v-btn size="small" color="error" variant="text" @click="deleteClassification(rule.id)">{{ tt('Delete') }}</v-btn></td></tr><tr v-if="!store.classifications.length"><td colspan="6" class="text-center text-medium-emphasis py-8">{{ tt('No classification rules') }}</td></tr></tbody>
                        </v-table>
                    </v-card>
                </v-window-item>

                <v-window-item value="review">
                    <v-card>
                        <v-card-title class="d-flex align-center"><span>{{ tt('Review & Audit') }}</span><v-spacer /><v-btn variant="text" prepend-icon="$refresh" @click="store.reloadCandidates">{{ tt('Refresh') }}</v-btn></v-card-title>
                        <v-table><thead><tr><th>{{ tt('Merchant') }}</th><th>{{ tt('Amount') }}</th><th>{{ tt('Status') }}</th><th>{{ tt('Updated') }}</th><th class="text-right">{{ tt('Actions') }}</th></tr></thead>
                            <tbody><tr v-for="candidate in store.candidates" :key="candidate.id"><td>{{ candidate.variants[0]?.merchant || '—' }}</td><td>{{ formatVariantAmount(candidate.variants[0]) }}</td><td><v-chip size="small" variant="tonal">{{ candidate.status }}</v-chip></td><td>{{ formatUnix(candidate.updatedUnixTime) }}</td><td class="text-right"><v-btn size="small" variant="text" @click="openAudit(candidate.id)">{{ tt('Audit') }}</v-btn><v-btn size="small" variant="text" @click="openCandidate(candidate)">{{ tt('Review') }}</v-btn><v-btn v-if="candidate.status === 'import_failed'" size="small" variant="text" @click="retryCandidate(candidate.id)">{{ tt('Retry') }}</v-btn></td></tr><tr v-if="!store.candidates.length"><td colspan="5" class="text-center text-medium-emphasis py-8">{{ tt('No email bill candidates') }}</td></tr></tbody>
                        </v-table>
                    </v-card>
                </v-window-item>
            </v-window>
        </v-col>
    </v-row>

    <v-dialog v-model="parserDialog" max-width="960" scrollable>
        <v-card :title="parserDraft.id ? tt('Edit Parser Rule') : tt('Add Parser Rule')">
            <v-card-text><v-row><v-col cols="12" md="5"><v-text-field :label="tt('Name')" v-model.trim="parserDraft.name" /></v-col><v-col cols="12" md="4"><v-text-field :label="tt('Bank')" v-model.trim="parserDraft.bank" /></v-col><v-col cols="12" md="3"><v-text-field type="number" :label="tt('Priority')" v-model.number="parserDraft.priority" /></v-col><v-col cols="12" md="6"><v-text-field :label="tt('Sender Matchers (comma separated)')" v-model="parserSenders" /></v-col><v-col cols="12" md="6"><v-text-field :label="tt('Subject Contains (comma separated)')" v-model="parserSubjects" /></v-col><v-col cols="12"><v-textarea class="code-editor" rows="15" :label="tt('Starlark Parser Code')" v-model="parserDraft.sourceCode" /></v-col></v-row>
                <v-divider class="my-5" />
                <div class="text-h6 mb-3">{{ tt('Parser Test Bench') }}</div>
                <v-select clearable :label="tt('Choose a Stored Email')" :items="messageOptions" item-title="title" item-value="value" v-model="testMessageId" @update:model-value="selectTestMessage" />
                <v-row><v-col cols="12" md="6"><v-text-field :label="tt('Sender')" v-model="testMail.sender" /></v-col><v-col cols="12" md="6"><v-text-field :label="tt('Subject')" v-model="testMail.subject" /></v-col><v-col cols="12"><v-textarea rows="8" :label="tt('Email Body')" v-model="testMail.text" /></v-col></v-row>
                <v-alert v-if="preview" class="mt-4" :type="preview.matched ? 'success' : 'info'" variant="tonal"><strong>{{ preview.matched ? tt('Matched') : tt('Not Matched') }}</strong> · {{ preview.bills.length }} {{ tt('bills') }} · {{ preview.durationMillis }} ms<pre v-if="preview.bills.length" class="preview-json mt-3">{{ JSON.stringify(preview.bills, null, 2) }}</pre></v-alert>
            </v-card-text>
            <v-card-actions><v-btn :loading="testing" @click="testParser">{{ tt('Test Only') }}</v-btn><v-spacer /><v-btn variant="text" @click="parserDialog = false">{{ tt('Cancel') }}</v-btn><v-btn color="primary" :loading="saving" @click="saveParser">{{ tt('Save') }}</v-btn></v-card-actions>
        </v-card>
    </v-dialog>

    <v-dialog v-model="routeDialog" max-width="760"><v-card :title="routeDraft.id ? tt('Edit Routing Rule') : tt('Add Routing Rule')"><v-card-text><v-row><v-col cols="12" md="6"><v-select :label="tt('Target Account')" :items="accountOptions" item-title="title" item-value="value" v-model="routeDraft.targetAccountId" /></v-col><v-col cols="12" md="3"><v-text-field type="number" :label="tt('Priority')" v-model.number="routeDraft.priority" /></v-col><v-col cols="12" md="3"><v-switch color="primary" :label="tt('Enabled')" v-model="routeDraft.enabled" /></v-col><v-col cols="12" md="6"><v-text-field :label="tt('Bank')" v-model.trim="routeDraft.bank" /></v-col><v-col cols="12" md="6"><v-text-field :label="tt('Account Kind')" v-model.trim="routeDraft.kind" /></v-col><v-col cols="12" md="6"><v-text-field :label="tt('Last Four Digits')" v-model.trim="routeDraft.last4" /></v-col><v-col cols="12" md="6"><v-text-field :label="tt('Currency')" v-model.trim="routeDraft.currency" /></v-col></v-row></v-card-text><v-card-actions><v-spacer /><v-btn variant="text" @click="routeDialog = false">{{ tt('Cancel') }}</v-btn><v-btn color="primary" :loading="saving" @click="saveRoute">{{ tt('Save') }}</v-btn></v-card-actions></v-card></v-dialog>

    <v-dialog v-model="classificationDialog" max-width="760"><v-card :title="classificationDraft.id ? tt('Edit Classification Rule') : tt('Add Classification Rule')"><v-card-text><v-row><v-col cols="12" md="8"><v-text-field :label="tt('Merchant Pattern')" v-model.trim="classificationDraft.merchantPattern" /></v-col><v-col cols="12" md="4"><v-select :label="tt('Match Type')" :items="matchTypes" v-model="classificationDraft.matchType" /></v-col><v-col cols="12" md="6"><v-select :label="tt('Category')" :items="categoryOptions" item-title="title" item-value="value" v-model="classificationDraft.categoryId" /></v-col><v-col cols="12" md="3"><v-text-field type="number" :label="tt('Priority')" v-model.number="classificationDraft.priority" /></v-col><v-col cols="12" md="3"><v-switch color="primary" :label="tt('Enabled')" v-model="classificationDraft.enabled" /></v-col><v-col cols="12" md="4"><v-text-field :label="tt('Bank Scope')" v-model.trim="classificationDraft.bank" /></v-col><v-col cols="12" md="4"><v-select clearable :label="tt('Account Scope')" :items="accountOptions" item-title="title" item-value="value" v-model="classificationDraft.accountId" /></v-col><v-col cols="12" md="4"><v-select clearable :label="tt('Flow Type')" :items="flowTypes" v-model="classificationDraft.flowType" /></v-col></v-row></v-card-text><v-card-actions><v-spacer /><v-btn variant="text" @click="classificationDialog = false">{{ tt('Cancel') }}</v-btn><v-btn color="primary" :loading="saving" @click="saveClassification">{{ tt('Save') }}</v-btn></v-card-actions></v-card></v-dialog>

    <v-dialog v-model="candidateDialog" max-width="720"><v-card :title="tt('Confirm Email Bill')"><v-card-text><v-select :label="tt('Parsed Variant')" :items="candidateVariantOptions" item-title="title" item-value="value" v-model="confirmation.variantId" /><v-select :label="tt('Target Account')" :items="accountOptions" item-title="title" item-value="value" v-model="confirmation.accountId" /><v-select :label="tt('Category')" :items="categoryOptions" item-title="title" item-value="value" v-model="confirmation.categoryId" /><v-alert type="info" variant="tonal">{{ tt('Confirmation creates an editable learned merchant rule and imports exactly once.') }}</v-alert></v-card-text><v-card-actions><v-spacer /><v-btn variant="text" @click="candidateDialog = false">{{ tt('Cancel') }}</v-btn><v-btn color="primary" :loading="saving" @click="confirmCandidate">{{ tt('Confirm & Import') }}</v-btn></v-card-actions></v-card></v-dialog>

    <v-dialog v-model="auditDialog" max-width="820" scrollable><v-card :title="tt('Audit Chain')"><v-card-text><v-timeline side="end" density="compact"><v-timeline-item v-for="event in auditEvents" :key="event.id" dot-color="primary" size="small"><div class="d-flex ga-3"><strong>{{ event.eventType }}</strong><span class="text-medium-emphasis">{{ formatUnix(event.createdUnixTime) }}</span></div><div class="text-caption">{{ event.actorType }}</div><pre class="audit-json">{{ JSON.stringify(event.payload, null, 2) }}</pre></v-timeline-item></v-timeline><div v-if="!auditEvents.length" class="text-center text-medium-emphasis py-8">{{ tt('No audit events') }}</div></v-card-text><v-card-actions><v-spacer /><v-btn @click="auditDialog = false">{{ tt('Close') }}</v-btn></v-card-actions></v-card></v-dialog>

    <snack-bar ref="snackbar" />
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref, useTemplateRef } from 'vue';

import SnackBar from '@/components/desktop/SnackBar.vue';
import { useI18n } from '@/locales/helpers.ts';
import services from '@/lib/services.ts';
import { buildEmailBillSchedule, createEmailBillParserRule, parseEmailBillSchedule, type EmailBillAuditEvent, type EmailBillCandidate, type EmailBillCandidateVariant, type EmailBillClassificationRule, type EmailBillParserPreview, type EmailBillParserRule, type EmailBillRoutingRule, type EmailBillScheduleMode, type EmailBillSettings } from '@/core/emailBill.ts';
import type { AccountInfoResponse } from '@/models/account.ts';
import type { TransactionCategoryInfoResponse } from '@/models/transaction_category.ts';
import { useEmailBillStore } from '@/stores/emailBill.ts';

type SnackBarType = InstanceType<typeof SnackBar>;
type SelectOption = { title: string; value: string };

const { tt } = useI18n();
const store = useEmailBillStore();
const snackbar = useTemplateRef<SnackBarType>('snackbar');

const activeTab = ref('mailbox');
const loading = ref(false);
const saving = ref(false);
const running = ref(false);
const testing = ref(false);
const parserDialog = ref(false);
const routeDialog = ref(false);
const classificationDialog = ref(false);
const candidateDialog = ref(false);
const auditDialog = ref(false);
const mailPassword = ref('');
const scheduleMode = ref<EmailBillScheduleMode>('daily');
const scheduleTime = ref('08:00');
const scheduleWeekday = ref('1');
const accounts = ref<AccountInfoResponse[]>([]);
const categories = ref<TransactionCategoryInfoResponse[]>([]);
const auditEvents = ref<EmailBillAuditEvent[]>([]);
const preview = ref<EmailBillParserPreview | null>(null);
const testMessageId = ref<string | null>(null);
const parserSenders = ref('');
const parserSubjects = ref('');

const settings = reactive<EmailBillSettings>({ enabled: false, imapServer: '', imapPort: 993, mailUser: '', passwordConfigured: false, timezone: 'Asia/Shanghai', cronExpression: '0 8 * * *', maxEmails: 50, requireAuthenticationResults: true, trustedAuthservDomains: [], retainRawEmails: false, rawEmailRetentionDays: 30 });
const parserDraft = reactive<EmailBillParserRule>(createEmailBillParserRule());
const routeDraft = reactive<EmailBillRoutingRule>(emptyRoute());
const classificationDraft = reactive<EmailBillClassificationRule>(emptyClassification());
const testMail = reactive({ messageId: '', sender: '', subject: '', receivedAt: new Date().toISOString(), text: '', headers: {} as Record<string, string> });
const confirmation = reactive({ candidateId: '', variantId: '', accountId: '', categoryId: '' });

const scheduleModes = computed(() => [{ title: tt('Daily'), value: 'daily' }, { title: tt('Weekly'), value: 'weekly' }, { title: tt('Advanced Cron'), value: 'advanced' }]);
const weekdays = computed(() => ['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday'].map((day, value) => ({ title: tt(day), value: String(value) })));
const matchTypes = ['exact', 'contains', 'regex'];
const flowTypes = ['expense', 'income', 'refund', 'transfer_in', 'transfer_out'];
const accountOptions = computed<SelectOption[]>(() => flattenAccounts(accounts.value));
const categoryOptions = computed<SelectOption[]>(() => flattenCategories(categories.value));
const messageOptions = computed<SelectOption[]>(() => store.messages.map(item => ({ title: `${item.subject || tt('No Subject')} · ${item.sender}`, value: item.id })));
const selectedCandidate = ref<EmailBillCandidate | null>(null);
const candidateVariantOptions = computed<SelectOption[]>(() => (selectedCandidate.value?.variants || []).map(item => ({ title: `${item.merchant || item.description || tt('Unknown')} · ${formatVariantAmount(item)}`, value: item.id })));

onMounted(loadPage);

async function loadPage(): Promise<void> {
    loading.value = true;
    try {
        const [accountResponse, categoryResponse] = await Promise.all([services.getAllAccounts({ visibleOnly: true }), services.getAllTransactionCategories(), store.loadAll()]);
        accounts.value = resultOf(accountResponse);
        categories.value = Object.values(resultOf(categoryResponse)).flat();
        if (store.settings) Object.assign(settings, store.settings);
        readSchedule(settings.cronExpression);
    } catch (error) { showError(error); } finally { loading.value = false; }
}

async function saveSettings(): Promise<void> {
    saving.value = true;
    try {
        settings.cronExpression = buildSchedule();
        await store.saveSettings({ ...settings, mailPassword: mailPassword.value || undefined, trustedAuthservDomains: [...settings.trustedAuthservDomains] });
        mailPassword.value = '';
        if (store.settings) Object.assign(settings, store.settings);
        snackbar.value?.showMessage('Data has been updated');
    } catch (error) { showError(error); } finally { saving.value = false; }
}

async function runNow(): Promise<void> { running.value = true; try { resultOf(await services.runEmailBillImport()); await store.reloadCandidates(); snackbar.value?.showMessage('Data has been updated'); } catch (error) { showError(error); } finally { running.value = false; } }

function openParser(rule?: EmailBillParserRule): void { Object.assign(parserDraft, rule ? JSON.parse(JSON.stringify(rule)) : createEmailBillParserRule()); parserSenders.value = parserDraft.matcher.senders.join(', '); parserSubjects.value = parserDraft.matcher.subjectContains.join(', '); preview.value = null; parserDialog.value = true; }
async function saveParser(): Promise<void> { saving.value = true; try { parserDraft.matcher = { senders: splitList(parserSenders.value), subjectContains: splitList(parserSubjects.value) }; await store.saveParser({ ...parserDraft }); parserDialog.value = false; snackbar.value?.showMessage('Data has been updated'); } catch (error) { showError(error); } finally { saving.value = false; } }
async function testParser(): Promise<void> { testing.value = true; try { preview.value = await store.testParser({ matcher: { senders: splitList(parserSenders.value), subjectContains: splitList(parserSubjects.value) }, sourceCode: parserDraft.sourceCode, mail: { ...testMail } }); } catch (error) { showError(error); } finally { testing.value = false; } }
async function disableParser(id: string): Promise<void> { try { await store.disableParser(id); } catch (error) { showError(error); } }

function selectTestMessage(id: string | null): void { const item = store.messages.find(message => message.id === id); if (item) Object.assign(testMail, { messageId: item.messageId, sender: item.sender, subject: item.subject, receivedAt: item.receivedAt, text: item.text, headers: {} }); }

function openRoute(rule?: EmailBillRoutingRule): void { Object.assign(routeDraft, rule ? { ...rule } : emptyRoute()); routeDialog.value = true; }
async function saveRoute(): Promise<void> { saving.value = true; try { await store.saveRoute({ ...routeDraft }); routeDialog.value = false; snackbar.value?.showMessage('Data has been updated'); } catch (error) { showError(error); } finally { saving.value = false; } }
async function disableRoute(id: string): Promise<void> { try { await store.disableRoute(id); } catch (error) { showError(error); } }

function openClassification(rule?: EmailBillClassificationRule): void { Object.assign(classificationDraft, rule ? { ...rule } : emptyClassification()); classificationDialog.value = true; }
async function saveClassification(): Promise<void> { saving.value = true; try { await store.saveClassification({ ...classificationDraft }); classificationDialog.value = false; snackbar.value?.showMessage('Data has been updated'); } catch (error) { showError(error); } finally { saving.value = false; } }
async function disableClassification(id: string): Promise<void> { try { await store.disableClassification(id); } catch (error) { showError(error); } }
async function deleteClassification(id: string): Promise<void> { try { await store.deleteClassification(id); snackbar.value?.showMessage('Data has been updated'); } catch (error) { showError(error); } }

function openCandidate(candidate: EmailBillCandidate): void { selectedCandidate.value = candidate; Object.assign(confirmation, { candidateId: candidate.id, variantId: candidate.selectedVariantId !== '0' ? candidate.selectedVariantId : candidate.variants[0]?.id || '', accountId: candidate.accountId !== '0' ? candidate.accountId : '', categoryId: candidate.categoryId !== '0' ? candidate.categoryId : '' }); candidateDialog.value = true; }
async function confirmCandidate(): Promise<void> { saving.value = true; try { await store.confirmCandidate({ ...confirmation }); candidateDialog.value = false; snackbar.value?.showMessage('Data has been updated'); } catch (error) { showError(error); } finally { saving.value = false; } }
async function retryCandidate(id: string): Promise<void> { try { await store.retryCandidate(id); } catch (error) { showError(error); } }
async function openAudit(id: string): Promise<void> { auditDialog.value = true; auditEvents.value = []; try { auditEvents.value = await store.loadAudit(id); } catch (error) { showError(error); } }

function emptyRoute(): EmailBillRoutingRule { return { id: '', enabled: true, priority: 0, versionId: '', version: 0, bank: '', kind: '', last4: '', currency: '', mailboxId: '0', targetAccountId: '', updatedUnixTime: 0 }; }
function emptyClassification(): EmailBillClassificationRule { return { id: '', origin: 'manual', enabled: true, priority: 0, versionId: '', version: 0, merchantPattern: '', matchType: 'exact', bank: '', accountId: '0', flowType: '', categoryId: '', confidence: 1, updatedUnixTime: 0 }; }
function splitList(value: string): string[] { return value.split(',').map(item => item.trim()).filter(Boolean); }
function flattenAccounts(items: AccountInfoResponse[], prefix = ''): SelectOption[] { return items.flatMap(item => [{ title: prefix + item.name, value: item.id }, ...flattenAccounts(item.subAccounts || [], `${prefix}${item.name} / `)]); }
function flattenCategories(items: TransactionCategoryInfoResponse[], prefix = ''): SelectOption[] { return items.flatMap(item => [{ title: prefix + item.name, value: item.id }, ...flattenCategories(item.subCategories || [], `${prefix}${item.name} / `)]); }
function accountName(id: string): string { return accountOptions.value.find(item => item.value === id)?.title || id || '—'; }
function categoryName(id: string): string { return categoryOptions.value.find(item => item.value === id)?.title || id || '—'; }
function routeSummary(rule: EmailBillRoutingRule): string { return [rule.bank, rule.kind, rule.last4 && `•••• ${rule.last4}`, rule.currency].filter(Boolean).join(' · ') || tt('Match Any'); }
function formatVariantAmount(item?: EmailBillCandidateVariant): string { return item ? `${(Math.abs(item.amount) / 100).toFixed(2)} ${item.currency}` : '—'; }
function formatUnix(value: number): string { return value ? new Date(value * 1000).toLocaleString() : '—'; }
function resultOf<T>(response: { data: { success: boolean; result: T } }): T { if (!response.data?.success) throw new Error('Email bill request failed'); return response.data.result; }
function showError(error: unknown): void { snackbar.value?.showError(error instanceof Error ? error : String(error)); }
function readSchedule(expression: string): void {
    const schedule = parseEmailBillSchedule(expression);
    scheduleMode.value = schedule.mode;
    scheduleTime.value = schedule.time;
    scheduleWeekday.value = schedule.weekday;
}

function buildSchedule(): string {
    return buildEmailBillSchedule(scheduleMode.value, scheduleTime.value, scheduleWeekday.value, settings.cronExpression);
}
</script>

<style scoped>
.code-editor :deep(textarea), .preview-json, .audit-json { font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; }
.preview-json, .audit-json { overflow: auto; white-space: pre-wrap; word-break: break-word; font-size: 0.78rem; }
.preview-json { max-height: 300px; }
.audit-json { margin-top: 4px; color: rgb(var(--v-theme-on-surface-variant)); }
</style>
