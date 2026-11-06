import { describe, expect, it } from 'vitest';
import { m } from '$lib/paraglide/messages';
import { formatDuration, formatMeterTime, formatPower, sessionUrl, stopMotiveLabel, transactionUrl } from './session';

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
