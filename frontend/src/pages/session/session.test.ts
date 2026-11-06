import { describe, expect, it } from 'vitest';
import { m } from '$lib/paraglide/messages';
import type { SessionDetail } from '$lib/contract.gen';
import { paymentRecord } from '../../test/fixtures';
import {
	chargeComparison,
	chargeLabel,
	formatDuration,
	formatMeterTime,
	formatPower,
	sessionUrl,
	stopMotiveLabel,
	transactionUrl
} from './session';

/** Only the cost matters to the charge comparison. */
const costing = (cost: number) => ({ cost }) as SessionDetail;

describe('charge comparison', () => {
	it('flags a payment settled for less than the session cost', () => {
		const comparison = chargeComparison(costing(2.37), paymentRecord({ amount: 31 }));
		expect(comparison).toEqual({ check: 'short', charged: 31, cost: 237, difference: -206 });
		expect(chargeLabel(comparison!)).toBe(m.session_charge_short({ charged: '0,31 €', cost: '2,37 €', difference: '2,06 €' }));
	});

	it('flags a payment settled for more than the session cost', () => {
		const comparison = chargeComparison(costing(1.5), paymentRecord({ amount: 400 }));
		expect(comparison?.check).toBe('over');
		expect(chargeLabel(comparison!)).toBe(m.session_charge_over({ charged: '4,00 €', cost: '1,50 €', difference: '2,50 €' }));
	});

	it('takes a cent apart as rounding, even for float costs', () => {
		expect(chargeComparison(costing(2.6), paymentRecord({ amount: 260 }))?.check).toBe('ok');
		expect(chargeComparison(costing(0.29), paymentRecord({ amount: 29 }))?.check).toBe('ok'); // 0.29 * 100 = 28.999…
		expect(chargeComparison(costing(2.6), paymentRecord({ amount: 259 }))?.check).toBe('ok');
		expect(chargeLabel(chargeComparison(costing(2.6), paymentRecord({ amount: 261 }))!)).toBe(m.session_charge_ok());
		expect(chargeComparison(costing(2.6), paymentRecord({ amount: 258 }))?.check).toBe('short');
	});

	it('compares only settled payments', () => {
		for (const status of ['PENDING', 'AUTHORIZED', 'CANCELED', 'ERROR']) {
			expect(chargeComparison(costing(2.37), paymentRecord({ status, amount: 31 }))).toBeNull();
		}
		expect(chargeComparison(costing(2.37), paymentRecord({ amount: null }))).toBeNull();
	});
});

describe('session page helpers', () => {
	it('builds the addresses the Go server serves (station names escaped as url.PathEscape does)', () => {
		expect(sessionUrl(990001, 'ST 1/A', 2, 4321)).toBe('/emsp-dashboard/sessions/990001/ST%201%2FA/2/4321');
		expect(transactionUrl(4321)).toBe('/emsp-dashboard/sessions/4321');
	});

	it('names the stop motives of the CSMS, in any case', () => {
		expect(stopMotiveLabel('CAR')).toBe(m.session_stop_car());
		expect(stopMotiveLabel('Cost')).toBe(m.session_stop_cost());
		expect(stopMotiveLabel('')).toBe(m.session_stop_unknown());
		expect(stopMotiveLabel('NEW_ONE')).toBe('NEW_ONE');
	});

	it('formats durations, power and meter times', () => {
		expect(formatDuration(null)).toBe('—');
		expect(formatDuration(35)).toBe('35 s');
		expect(formatDuration(12 * 60 + 20)).toBe('12 min');
		expect(formatDuration(100 * 60)).toBe('1 h 40 min');
		expect(formatDuration(26 * 3600 + 5 * 60)).toBe('26 h 5 min');
		expect(formatPower(7200)).toBe('7,2 kW');
		expect(formatPower(null)).toBe('—');
		expect(formatMeterTime('2026-11-04T19:01:00.000000+01:00')).toBe('04/11, 19:01:00');
		expect(formatMeterTime('t1')).toBe('t1');
	});
});
