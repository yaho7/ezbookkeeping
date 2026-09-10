export interface EmailBillSettings {
    folderMode: 'all' | 'selected';
    folders: string[];
    enabled: boolean;
    imapServer: string;
    imapPort: number;
    mailUser: string;
    mailPassword?: string;
    passwordConfigured: boolean;
    timezone: string;
    cronExpression: string;
    maxEmails: number;
    retainRawEmails: boolean;
    rawEmailRetentionDays: number;
}

export interface EmailBillMatcher {
    senders: string[];
    subjectContains: string[];
}

export interface EmailBillParserRule {
    id: string;
    name: string;
    bank: string;
    enabled: boolean;
    priority: number;
    versionId: string;
    version: number;
    matcher: EmailBillMatcher;
    sourceCode: string;
    runtimeVersion: string;
    createdBy: string;
    updatedUnixTime: number;
}

export interface EmailBillMessageSample {
    id: string;
    messageId: string;
    sender: string;
    subject: string;
    receivedAt: string;
    text: string;
    bodySummary: string;
    authenticationStatus: string;
}

export interface EmailBillSyncTask {
    id: string;
    status: string;
    stage: string;
    currentFolder: string;
    scanned: number;
    downloaded: number;
    processed: number;
    skipped: number;
    failed: number;
    errorMessage: string;
    startedUnixTime: number;
    updatedUnixTime: number;
    completedUnixTime: number;
    folders: Array<{ name: string; status: string; total: number; scanned: number }>;
}

export interface EmailBillScanMessage {
    id: string;
    folder: string;
    sender: string;
    subject: string;
    remoteMessageId: string;
    receivedUnixTime: number;
    updatedUnixTime: number;
    authenticated: boolean;
    status: string;
    reason: string;
    messageId: string;
    importRunId: string;
    taskId: string;
}

export interface EmailBillMailboxPage {
    messages: EmailBillScanMessage[];
    total: number;
    page: number;
    pageSize: number;
    folders: string[];
}

export interface EmailBillMailboxDetail {
    message: EmailBillScanMessage;
    text: string;
    bodySummary: string;
    receivedAt?: string;
    runStatus?: string;
    parsers: Array<{ name: string; version: number; status: string; matched: boolean; outputs: number; errorMessage: string; durationMillis: number }>;
    candidates: Array<{ id: string; status: string; amount: number; currency: string; merchant: string }>;
}

export interface EmailBillParserPreview {
    matched: boolean;
    executionSteps: number;
    durationMillis: number;
    bills: Array<{
        bill: Record<string, unknown>;
        candidateIdentity: string;
        billFingerprint: string;
    }>;
}

export interface EmailBillGeneratedParser {
    name: string;
    bank: string;
    matcher: EmailBillMatcher;
    sourceCode: string;
    preview: EmailBillParserPreview;
}

export interface EmailBillRoutingRule {
    id: string;
    enabled: boolean;
    priority: number;
    versionId: string;
    version: number;
    bank: string;
    kind: string;
    last4: string;
    currency: string;
    mailboxId: string;
    targetAccountId: string;
    updatedUnixTime: number;
}

export interface EmailBillClassificationRule {
    id: string;
    origin: 'manual' | 'learned_manual' | 'learned_llm';
    enabled: boolean;
    priority: number;
    versionId: string;
    version: number;
    merchantPattern: string;
    matchType: 'exact' | 'contains' | 'regex';
    bank: string;
    accountId: string;
    flowType: string;
    categoryId: string;
    confidence: number;
    updatedUnixTime: number;
}

export interface EmailBillCandidateVariant {
    id: string;
    amount: number;
    currency: string;
    direction: string;
    merchant: string;
    description: string;
    occurredAt: string;
    bank: string;
    kind: string;
    last4: string;
}

export interface EmailBillCandidate {
    id: string;
    status: string;
    selectedVariantId: string;
    variants: EmailBillCandidateVariant[];
    accountId: string;
    categoryId: string;
    transactionId: string;
    updatedUnixTime: number;
}

export interface EmailBillAuditEvent {
    id: string;
    eventType: string;
    actorType: string;
    payload: Record<string, unknown>;
    createdUnixTime: number;
}

export type EmailBillScheduleMode = 'daily' | 'weekly' | 'advanced';

export interface EmailBillSchedule {
    mode: EmailBillScheduleMode;
    time: string;
    weekday: string;
}

export function createEmailBillParserRule(): EmailBillParserRule {
    return {
        id: '', name: '', bank: '', enabled: true, priority: 0,
        versionId: '', version: 0,
        matcher: { senders: [], subjectContains: [] },
        sourceCode: 'def parse(mail):\n    return []',
        runtimeVersion: 'starlark-v1', createdBy: 'user', updatedUnixTime: 0
    };
}

export function normalizeEmailBillCandidate(candidate: Partial<EmailBillCandidate>): EmailBillCandidate {
    return {
        id: String(candidate.id || ''), status: String(candidate.status || ''),
        selectedVariantId: String(candidate.selectedVariantId || '0'),
        variants: candidate.variants || [], accountId: String(candidate.accountId || '0'),
        categoryId: String(candidate.categoryId || '0'), transactionId: String(candidate.transactionId || '0'),
        updatedUnixTime: Number(candidate.updatedUnixTime || 0)
    };
}

export function parseEmailBillSchedule(expression: string): EmailBillSchedule {
    const parts = expression.trim().split(/\s+/);
    const minute = parts[0] || '';
    const hour = parts[1] || '';
    const day = parts[2] || '';
    const month = parts[3] || '';
    const weekday = parts[4] || '';
    const simpleTime = parts.length === 5 && day === '*' && month === '*' && /^\d+$/.test(minute) && /^\d+$/.test(hour);
    if (!simpleTime) return { mode: 'advanced', time: '08:00', weekday: '1' };
    const time = `${hour.padStart(2, '0')}:${minute.padStart(2, '0')}`;
    if (weekday === '*') return { mode: 'daily', time, weekday: '1' };
    if (/^[0-6]$/.test(weekday)) return { mode: 'weekly', time, weekday };
    return { mode: 'advanced', time, weekday: '1' };
}

export function buildEmailBillSchedule(mode: EmailBillScheduleMode, time: string, weekday: string, advancedExpression: string): string {
    if (mode === 'advanced') return advancedExpression;
    const parts = time.split(':');
    const hour = parts[0] || '0';
    const minute = parts[1] || '0';
    return `${Number(minute)} ${Number(hour)} * * ${mode === 'weekly' ? weekday : '*'}`;
}
