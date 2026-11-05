import { describe, expect, it } from 'vitest';
import { addDays, defaultRange, displayStatus, errorText, formatCents, formatDateTime, summarize } from './payments';
import { paymentRecord as payment } from '../../test/fixtures';

const now = Date.parse('2026-11-05T12:00:00Z');

describe('payments', () => {
	it('defaults to the last seven days, across month ends', () => {
		expect(defaultRange('2026-11-05')).toEqual({ from: '2026-10-30', to: '2026-11-05' });
		expect(addDays('2026-03-01', -1)).toBe('2026-02-28');
	});

	it('shows an old PENDING payment as abandoned, and a recent one as pending', () => {
		expect(displayStatus(payment({ status: 'PENDING', created_at: '2026-11-05T11:00:00Z' }), now)).toBe('ABANDONED');
		expect(displayStatus(payment({ status: 'PENDING', created_at: '2026-11-05T11:50:00Z' }), now)).toBe('PENDING');
		expect(displayStatus(payment({ status: 'ERROR' }), now)).toBe('ERROR');
	});

	it('formats cents as euros and instants as Madrid time', () => {
		expect(formatCents(1234)).toBe('12,34 €');
		expect(formatCents(null)).toBe('—');
		expect(formatDateTime('2026-11-05T10:02:00+00:00')).toBe('05/11/2026, 11:02');
	});

	it('describes an error with its step and code', () => {
		const failed = payment({ error_stage: 'PREAUTH', error_code: 562, error_message: 'Paycomet refused the preauth' });

		expect(errorText(failed)).toBe('PREAUTH 562: Paycomet refused the preauth');
		expect(errorText(payment({ error_stage: 'CHECK', error_message: "The notification service didn't answer" }))).toBe(
			"CHECK: The notification service didn't answer"
		);
		expect(errorText(payment())).toBe('');
	});

	it('counts by status and adds up only the money captured on LIVE payments', () => {
		const summary = summarize(
			[
				payment({ amount: 1000 }),
				payment({ amount: 500, category: 'TEST' }),
				payment({ status: 'ERROR', amount: null }),
				payment({ status: 'PENDING', created_at: '2026-11-05T08:00:00Z', amount: null })
			],
			now
		);

		expect(summary.committedCents).toBe(1000);
		expect(summary.counts).toMatchObject({ COMMITTED: 2, ERROR: 1, ABANDONED: 1, PENDING: 0 });
	});
});
