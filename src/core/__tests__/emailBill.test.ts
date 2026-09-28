import { describe, expect, it } from 'vitest';

import { emailBillRuleSavePayload, buildEmailBillSchedule, createEmailBillParserRule, parseEmailBillSchedule } from '@/core/emailBill.ts';

describe('email bill settings models', () => {
    it('creates an enabled bounded parser draft', () => {
        const rule = createEmailBillParserRule();

        expect(rule.enabled).toBe(true);
        expect(rule.matcher.senders).toEqual([]);
        expect(rule.sourceCode).toContain('def parse(mail):');
    });

    it('round-trips daily and weekly schedules', () => {
        expect(parseEmailBillSchedule('30 8 * * *')).toEqual({ mode: 'daily', time: '08:30', weekday: '1' });
        expect(parseEmailBillSchedule('15 21 * * 5')).toEqual({ mode: 'weekly', time: '21:15', weekday: '5' });
        expect(buildEmailBillSchedule('weekly', '21:15', '5', '')).toBe('15 21 * * 5');
    });

    it('preserves advanced cron expressions', () => {
        expect(parseEmailBillSchedule('0 9 1 * *').mode).toBe('advanced');
        expect(buildEmailBillSchedule('advanced', '08:00', '1', '0 9 1 * *')).toBe('0 9 1 * *');
    });
});

describe('email bill rule request IDs', () => {
    it('encodes a new rule as zero without changing its draft', () => {
        const draft = { id: '', targetAccountId: '3841959312247226368' };
        expect(JSON.parse(JSON.stringify(emailBillRuleSavePayload(draft)))).toEqual({ id: '0', targetAccountId: draft.targetAccountId });
        expect(draft.id).toBe('');
        expect(emailBillRuleSavePayload({})).toEqual({ id: '0' });
    });

    it('preserves existing IDs beyond JavaScript integer precision', () => {
        const draft = { id: '3841959312247226368' };
        expect(emailBillRuleSavePayload(draft).id).toBe(draft.id);
    });
});
