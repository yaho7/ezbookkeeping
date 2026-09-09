import { describe, expect, it } from 'vitest';

import { buildEmailBillSchedule, createEmailBillParserRule, normalizeEmailBillCandidate, parseEmailBillSchedule } from '@/core/emailBill.ts';

describe('email bill settings models', () => {
    it('creates an enabled bounded parser draft', () => {
        const rule = createEmailBillParserRule();

        expect(rule.enabled).toBe(true);
        expect(rule.matcher.senders).toEqual([]);
        expect(rule.sourceCode).toContain('def parse(mail):');
    });

    it('keeps large candidate ids as strings', () => {
        const candidate = normalizeEmailBillCandidate({ id: '9007199254740993', variants: [] });

        expect(candidate.id).toBe('9007199254740993');
        expect(candidate.selectedVariantId).toBe('0');
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
