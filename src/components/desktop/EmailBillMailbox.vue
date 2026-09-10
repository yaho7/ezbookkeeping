<template>
    <v-card class="mail-workspace">
        <div class="pa-5">
            <div class="d-flex align-center flex-wrap ga-3">
                <div class="text-h6">{{ tt('Email Workspace') }}</div>
                <v-chip size="small" variant="tonal" :color="statusColor(task?.status || '')">{{ task ? taskStatus(task.status) : tt('Not started') }}</v-chip>
                <v-spacer />
                <span v-if="task" class="text-caption text-medium-emphasis">{{ tt('Updated') }} {{ formatTime(task.updatedUnixTime) }}</span>
                <v-btn variant="text" prepend-icon="$refresh" :loading="refreshing" @click="refresh">{{ tt('Refresh') }}</v-btn>
            </div>
            <p class="text-body-2 text-medium-emphasis mt-2">{{ tt('Scanning runs on the server. You can leave this page and return to see progress.') }}</p>
            <div v-if="task" class="d-flex flex-wrap ga-5 mt-4" aria-live="polite">
                <div v-for="counter in counters" :key="counter.label"><span class="text-h6 mr-2">{{ counter.value }}</span><span class="text-body-2 text-medium-emphasis">{{ tt(counter.label) }}</span></div>
            </div>
            <div v-if="active" class="mt-3 text-body-2">
                {{ tt(stageLabels[task?.stage || ''] || 'Running in background') }}<span v-if="task?.currentFolder"> · {{ task.currentFolder }}</span>
                <v-progress-linear class="mt-2" indeterminate color="primary" height="3" :aria-label="tt('Running in background')" />
            </div>
            <v-alert v-if="task?.errorMessage" class="mt-3" type="error" variant="tonal" density="compact">{{ tt(task.errorMessage) }}</v-alert>
            <v-alert v-if="errorMessage" class="mt-3" type="warning" variant="tonal" density="compact" aria-live="polite">{{ errorMessage }}</v-alert>
        </div>
        <v-divider />
        <div class="mail-panes">
            <aside class="mail-folders pa-3" :aria-label="tt('Folders')">
                <div class="text-overline px-3 mb-2">{{ tt('Folders') }}</div>
                <v-list density="compact" nav bg-color="transparent">
                    <v-list-item :active="!folder" color="primary" :title="tt('All Folders')" @click="chooseFolder('')" />
                    <v-list-item v-for="item in folderOptions" :key="item" :active="folder === item" color="primary" @click="chooseFolder(item)">
                        <v-list-item-title class="text-wrap">{{ item }}</v-list-item-title>
                        <v-list-item-subtitle v-if="folderProgress(item)" class="mt-1 text-wrap">{{ folderProgress(item) }}</v-list-item-subtitle>
                    </v-list-item>
                </v-list>
            </aside>
            <section class="mail-list" :aria-label="tt('Email List')" :aria-busy="listing">
                <div class="pa-4 mail-search">
                    <form class="d-flex ga-2 mb-3" @submit.prevent="applySearch">
                        <v-text-field v-model="searchDraft" :label="tt('Search sender or subject')" density="compact" hide-details maxlength="200" />
                        <v-btn type="submit" variant="tonal">{{ tt('Search') }}</v-btn>
                    </form>
                    <v-select v-model="status" :items="statusOptions" :label="tt('Processing Status')" density="compact" hide-details @update:model-value="filterChanged" />
                </div>
                <v-divider />
                <div class="mail-rows">
                    <div v-if="!messages.length" class="pa-6 text-center text-medium-emphasis">
                        {{ tt(listing ? 'Loading...' : 'No scanned emails match this view. Run a scan or change the filters.') }}
                    </div>
                    <button v-for="message in messages" :key="message.id" type="button" class="mail-row pa-4" :class="{ selected: selectedId === message.id }" :aria-pressed="selectedId === message.id" @click="selectMessage(message.id)">
                        <div class="d-flex justify-space-between ga-2 mb-1"><strong class="text-truncate">{{ message.sender || tt('Unknown Sender') }}</strong><span class="text-caption text-medium-emphasis flex-shrink-0">{{ formatDate(message.receivedUnixTime) }}</span></div>
                        <div class="text-truncate text-body-2 mb-2">{{ message.subject || tt('No Subject') }}</div>
                        <div class="d-flex flex-wrap align-center ga-2"><v-chip size="x-small" variant="tonal" :color="statusColor(message.status)">{{ messageStatus(message.status) }}</v-chip><span class="text-caption text-medium-emphasis text-truncate">{{ message.folder }}</span></div>
                    </button>
                </div>
                <div class="d-flex align-center justify-space-between pa-3 mail-pagination">
                    <v-btn size="small" variant="text" :disabled="page <= 1" @click="changePage(-1)">{{ tt('Previous') }}</v-btn>
                    <span class="text-caption">{{ page }} / {{ pageCount }} · {{ total }}</span>
                    <v-btn size="small" variant="text" :disabled="page >= pageCount" @click="changePage(1)">{{ tt('Next') }}</v-btn>
                </div>
            </section>
            <section class="mail-detail pa-5" :aria-label="tt('Email Details')" :aria-busy="detailLoading">
                <div v-if="detailLoading && !detail" class="text-medium-emphasis">{{ tt('Loading...') }}</div>
                <template v-else-if="detail">
                    <div class="d-flex flex-wrap ga-2 mb-3"><v-chip size="small" variant="tonal" :color="statusColor(detail.message.status)">{{ messageStatus(detail.message.status) }}</v-chip><v-chip size="small" variant="outlined">{{ tt(detail.message.authenticated ? 'Authentication passed' : 'Authentication not verified') }}</v-chip></div>
                    <h2 class="text-h6 mb-3 mail-subject">{{ detail.message.subject || tt('No Subject') }}</h2>
                    <div class="text-body-2 text-medium-emphasis mb-1">{{ detail.message.sender || tt('Unknown Sender') }}</div>
                    <div class="text-caption text-medium-emphasis mb-4">{{ formatTime(detail.message.receivedUnixTime) }} · {{ detail.message.folder }}</div>
                    <v-alert v-if="detail.message.reason" type="info" variant="tonal" density="compact" class="mb-4">{{ tt(detail.message.reason) }}</v-alert>
                    <h3 class="text-subtitle-1 mb-2">{{ tt('Parsing & Bookkeeping') }}</h3>
                    <p v-if="!detail.parsers.length" class="text-body-2 text-medium-emphasis mb-4">{{ tt('This email has no parser execution yet. See its processing status above.') }}</p>
                    <div v-for="(parser, index) in detail.parsers" :key="index" class="mail-evidence pa-3 mb-2">
                        <div class="d-flex flex-wrap justify-space-between ga-2"><strong>{{ parser.name }} <span class="text-caption">v{{ parser.version }}</span></strong><v-chip size="x-small" :color="statusColor(parser.status)">{{ messageStatus(parser.status) }}</v-chip></div>
                        <div class="text-caption mt-1">{{ parser.outputs }} {{ tt('bills') }} · {{ parser.durationMillis }} ms</div>
                        <p v-if="parser.errorMessage" class="text-body-2 text-error mt-2">{{ parser.errorMessage }}</p>
                    </div>
                    <div v-for="candidate in detail.candidates" :key="candidate.id" class="mail-evidence pa-3 mb-2 d-flex flex-wrap align-center ga-2">
                        <span>{{ candidate.merchant || tt('Unknown') }}</span><v-chip size="small" :color="statusColor(candidate.status)">{{ messageStatus(candidate.status) }}</v-chip><v-spacer /><v-btn size="small" variant="text" @click="emit('review')">{{ tt('Review & Audit') }}</v-btn>
                    </div>
                    <v-divider class="my-5" />
                    <div class="d-flex align-center flex-wrap ga-2 mb-3"><h3 class="text-subtitle-1">{{ tt('Email Body') }}</h3><v-spacer /><v-btn size="small" variant="text" :disabled="!detail.text" @click="emit('test', detail)">{{ tt('Open in Test Bench') }}</v-btn></div>
                    <pre v-if="detail.text" class="mail-body">{{ detail.text }}</pre>
                    <template v-else><p class="text-body-2 text-medium-emphasis">{{ tt('The full body was not retained or has expired. Only matching authenticated emails are downloaded; enable body retention in Mailbox & Schedule for future scans.') }}</p><p v-if="detail.bodySummary" class="mail-body mt-3">{{ detail.bodySummary }}</p></template>
                    <div class="text-caption text-disabled mt-5 mail-subject">{{ detail.message.remoteMessageId }}</div>
                </template>
                <div v-else class="mail-empty text-medium-emphasis">{{ tt('Select an email to inspect its content and processing results.') }}</div>
            </section>
        </div>
    </v-card>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue';
import { isAxiosError } from 'axios';
import { useI18n } from '@/locales/helpers.ts';
import services from '@/lib/services.ts';
import type { EmailBillMailboxDetail, EmailBillScanMessage, EmailBillSyncTask } from '@/core/emailBill.ts';

const emit = defineEmits<{ active: [value: boolean]; review: []; test: [detail: EmailBillMailboxDetail] }>();
const { tt } = useI18n();
const task = ref<EmailBillSyncTask | null>(null);
const messages = ref<EmailBillScanMessage[]>([]);
const folders = ref<string[]>([]);
const detail = ref<EmailBillMailboxDetail | null>(null);
const folder = ref('');
const status = ref('');
const searchDraft = ref('');
const search = ref('');
const selectedId = ref('');
const page = ref(1);
const total = ref(0);
const listing = ref(false);
const refreshing = ref(false);
const detailLoading = ref(false);
const errorMessage = ref('');
let alive = true;
let timer: ReturnType<typeof setTimeout> | undefined;
let listRequest = 0;
let detailRequest = 0;
let taskVersion = 0;
let starting = false;
const active = computed(() => task.value?.status === 'queued' || task.value?.status === 'running');
const pageCount = computed(() => Math.max(1, Math.ceil(total.value / 50)));
const folderOptions = computed(() => [...new Set([...(task.value?.folders || []).map(item => item.name), ...folders.value])]);
const counters = computed(() => [
    { label: 'Scanned', value: task.value?.scanned || 0 }, { label: 'Downloaded', value: task.value?.downloaded || 0 },
    { label: 'Processed', value: task.value?.processed || 0 }, { label: 'Skipped', value: task.value?.skipped || 0 }, { label: 'Failed', value: task.value?.failed || 0 }
]);
const labels: Record<string, string> = { ready: 'Waiting for download', downloaded: 'Downloaded', processing: 'Processing', succeeded: 'Parsed', no_output: 'No bills parsed', not_matched: 'Not Matched', rejected: 'Authentication rejected', duplicate: 'Already processed', oversized: 'Message too large', failed: 'Failed', partial_success: 'Partially completed', not_processed: 'Not processed', imported: 'Imported', awaiting_account: 'Awaiting account', awaiting_classification: 'Awaiting classification', awaiting_confirmation: 'Awaiting confirmation', import_failed: 'Import failed', conflict: 'Conflict' };
const stageLabels: Record<string, string> = { queued: 'Queued', connecting: 'Connecting to mailbox', scanning: 'Scanning folders', processing: 'Parsing & Bookkeeping' };
const statusOptions = computed(() => [{ title: tt('All Statuses'), value: '' }, ...['succeeded', 'no_output', 'not_matched', 'rejected', 'duplicate', 'oversized', 'failed', 'partial_success', 'not_processed', 'ready', 'downloaded', 'processing'].map(value => ({ title: messageStatus(value), value }))]);

watch(active, value => emit('active', value), { immediate: true });
onMounted(() => { void refresh(); });
onUnmounted(() => { alive = false; listRequest++; detailRequest++; clearTimeout(timer); });

function resultOf<T>(response: { data: { success: boolean; result: T } }): T {
    if (!response.data.success) throw new Error('Email bill request failed');
    return response.data.result;
}
function reportError(error: unknown): void {
    if (!alive) return;
    const data = isAxiosError<{ errorMessage?: string }>(error) ? error.response?.data : undefined;
    errorMessage.value = tt(data?.errorMessage || (error instanceof Error ? error.message : 'Email bill request failed'));
}
function messageStatus(value: string): string { return tt(labels[value] || value); }
function taskStatus(value: string): string {
    const names: Record<string, string> = { queued: 'Queued', running: 'Running in background', succeeded: 'Scan completed', partial_success: 'Partially completed', failed: 'Failed', interrupted: 'Interrupted' };
    return tt(names[value] || value);
}
function statusColor(value: string): string | undefined {
    if (['failed', 'import_failed', 'rejected', 'interrupted'].includes(value)) return 'error';
    if (['partial_success', 'awaiting_account', 'awaiting_classification', 'awaiting_confirmation', 'conflict', 'not_processed'].includes(value)) return 'warning';
    if (['succeeded', 'imported'].includes(value)) return 'success';
    if (['running', 'queued', 'processing', 'downloaded', 'ready'].includes(value)) return 'primary';
    return undefined;
}
function formatTime(value: number): string { return value ? new Date(value * 1000).toLocaleString() : '—'; }
function formatDate(value: number): string { return value ? new Date(value * 1000).toLocaleDateString() : '—'; }
function folderProgress(name: string): string {
    const item = task.value?.folders.find(value => value.name === name);
    if (!item) return '';
    const names: Record<string, string> = { waiting: 'Queued', opening: 'Opening folder', scanning: 'Scanning folders', completed: 'Scan completed', limited: 'Processing limit reached', interrupted: 'Interrupted', not_scanned: 'Not scanned' };
    const total = ['waiting', 'opening', 'not_scanned'].includes(item.status) ? '—' : item.total;
    return `${tt(names[item.status] || item.status)} · ${item.scanned} / ${total}`;
}
async function loadList(): Promise<void> {
    const request = ++listRequest;
    listing.value = true;
    try {
        const result = resultOf(await services.listEmailBillMailbox({ folder: folder.value, status: status.value, search: search.value, page: page.value }));
        if (!alive || request !== listRequest) return;
        messages.value = result.messages;
        folders.value = result.folders;
        total.value = result.total;
        if (!selectedId.value && result.messages[0]) selectMessage(result.messages[0].id);
    } catch (error) { if (request === listRequest) reportError(error); }
    finally { if (alive && request === listRequest) listing.value = false; }
}
async function loadDetail(): Promise<void> {
    if (!selectedId.value) return;
    const request = ++detailRequest;
    const id = selectedId.value;
    detailLoading.value = true;
    try {
        const result = resultOf(await services.getEmailBillMailboxDetail(id));
        if (alive && request === detailRequest && selectedId.value === id) detail.value = result;
    } catch (error) { if (request === detailRequest) reportError(error); }
    finally { if (alive && request === detailRequest) detailLoading.value = false; }
}
async function refresh(): Promise<void> {
    if (!alive || refreshing.value) return;
    clearTimeout(timer);
    refreshing.value = true;
    errorMessage.value = '';
    const version = taskVersion;
    try {
        const result = resultOf(await services.getEmailBillSyncStatus());
        if (!alive) return;
        if (version === taskVersion) task.value = result;
        await Promise.all([loadList(), loadDetail()]);
    } catch (error) { reportError(error); }
    finally {
        if (alive) {
            refreshing.value = false;
            timer = setTimeout(() => { void refresh(); }, active.value ? 3000 : 15000);
        }
    }
}
async function start(): Promise<void> {
    if (starting || active.value) { await refresh(); return; }
    starting = true;
    taskVersion++;
    errorMessage.value = '';
    try {
        const result = resultOf(await services.runEmailBillImport());
        if (alive) task.value = result;
    } catch (error) { reportError(error); }
    finally { starting = false; void refresh(); }
}
function clearSelection(): void { selectedId.value = ''; detail.value = null; detailRequest++; detailLoading.value = false; }
function filterChanged(): void { page.value = 1; clearSelection(); void loadList(); }
function chooseFolder(value: string): void { folder.value = value; filterChanged(); }
function applySearch(): void { search.value = searchDraft.value; filterChanged(); }
function changePage(delta: number): void { page.value += delta; clearSelection(); void loadList(); }
function selectMessage(id: string): void { selectedId.value = id; detail.value = null; void loadDetail(); }
defineExpose({ start, refresh });
</script>

<style scoped>
.mail-panes { display: grid; grid-template-columns: 200px minmax(280px, 360px) minmax(0, 1fr); min-height: 620px; }
.mail-folders { background: rgba(var(--v-theme-on-surface), 0.025); border-inline-end: thin solid rgba(var(--v-border-color), var(--v-border-opacity)); }
.mail-list { min-width: 0; display: flex; flex-direction: column; border-inline-end: thin solid rgba(var(--v-border-color), var(--v-border-opacity)); }
.mail-rows { flex: 1; max-height: 620px; overflow-y: auto; }
.mail-row { display: block; width: 100%; text-align: start; border-block-end: thin solid rgba(var(--v-border-color), var(--v-border-opacity)); cursor: pointer; }
.mail-row:hover { background: rgba(var(--v-theme-on-surface), 0.04); }
.mail-row.selected { background: rgba(var(--v-theme-primary), 0.09); box-shadow: inset 3px 0 rgb(var(--v-theme-primary)); }
.mail-row:focus-visible { outline: 2px solid rgb(var(--v-theme-primary)); outline-offset: -2px; }
.mail-pagination { border-block-start: thin solid rgba(var(--v-border-color), var(--v-border-opacity)); }
.mail-detail { min-width: 0; max-height: 820px; overflow-y: auto; }
.mail-subject, .mail-evidence { overflow-wrap: anywhere; }
.mail-evidence { border: thin solid rgba(var(--v-border-color), var(--v-border-opacity)); border-radius: 6px; }
.mail-body { white-space: pre-wrap; overflow-wrap: anywhere; font: inherit; font-size: 0.875rem; line-height: 1.8; }
.mail-empty { display: grid; place-items: center; min-height: 400px; text-align: center; }
@media (max-width: 1250px) { .mail-panes { grid-template-columns: 160px minmax(240px, 300px) minmax(0, 1fr); } }
@media (max-width: 960px) { .mail-panes { grid-template-columns: 170px minmax(0, 1fr); } .mail-detail { grid-column: 1 / -1; border-block-start: thin solid rgba(var(--v-border-color), var(--v-border-opacity)); } .mail-rows { max-height: 380px; } .mail-empty { min-height: 120px; } }
@media (max-width: 600px) { .mail-panes { display: block; } .mail-folders { max-height: 200px; overflow-y: auto; border-block-end: thin solid rgba(var(--v-border-color), var(--v-border-opacity)); } }
</style>
