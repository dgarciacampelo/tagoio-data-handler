import { describe, expect, it } from 'vitest';
import type { SessionSeries } from '$lib/contract.gen';
import { MARGIN, PLOT_W, chartGeometry, decimals, niceMax, stepAt, stepValues, utcOffset } from './chart';

/** A series of one-minute steps from 18:00 UTC (19:00 in Madrid in November), from the power of each step (W). */
function series(watts: (number | null)[]): SessionSeries {
	let total = 0;
	return {
		start: '2026-11-04T18:00:00Z',
		step: 60,
		power: watts,
		energy: watts.map((w) => {
			if (w === null) return null;
			total += w / 60;
			return Math.round(total);
		})
	};
}

describe('chart scales', () => {
	it('rounds the maximum up to even gridlines, as the email does', () => {
		expect(niceMax(11)).toBe(12);
		expect(niceMax(7.2)).toBe(8);
		expect(niceMax(6)).toBe(6);
		expect(niceMax(0.35)).toBe(0.4);
		expect(niceMax(0)).toBe(1);
	});

	it('shows each gridline step with the decimals it needs', () => {
		expect(decimals(3)).toBe(0);
		expect(decimals(0.25)).toBe(2);
		expect(decimals(1 / 3)).toBe(3);
	});

	it("knows Madrid's offset in winter and summer", () => {
		expect(utcOffset(Date.parse('2026-11-04T18:00:00Z'))).toBe(3600_000);
		expect(utcOffset(Date.parse('2026-07-04T18:00:00Z'))).toBe(7200_000);
	});
});

describe('chartGeometry', () => {
	it('labels the gridlines and the local times', () => {
		const geometry = chartGeometry(series(Array(60).fill(6000)));
		// Every label with the decimals of the step, as the email prints them
		expect(geometry.gridlines.map((g) => g.power)).toEqual(['0,0', '1,5', '3,0', '4,5', '6,0']);
		expect(geometry.gridlines.map((g) => g.energy)).toEqual(['0,0', '1,5', '3,0', '4,5', '6,0']);
		// An hour fits six 10 min ticks, after 19:00 local (the start itself unlabelled, as in the email)
		expect(geometry.ticks.map((t) => t.label)).toEqual(['19:10', '19:20', '19:30', '19:40', '19:50']);
		expect(geometry.ticks[2].x).toBeCloseTo(MARGIN.left + PLOT_W / 2);
	});

	it('breaks the power area and the energy line at a gap', () => {
		const geometry = chartGeometry(series([7000, 7000, null, null, 7000, 7000]));
		expect(geometry.powerAreas).toHaveLength(2);
		expect(geometry.powerLines).toHaveLength(2);
		expect(geometry.energyLines).toHaveLength(2);
		// The first energy line starts from nothing delivered, at the start of the first step
		expect(geometry.energyLines[0].startsWith(`M${MARGIN.left} `)).toBe(true);
	});

	it('finds the step under the pointer and its values', () => {
		const s = series([6000, null, 3000]);
		expect(stepAt(s, MARGIN.left - 1)).toBeNull();
		expect(stepAt(s, MARGIN.left + PLOT_W / 6)).toBe(0);
		expect(stepAt(s, MARGIN.left + PLOT_W - 1)).toBe(2);
		expect(stepAt(s, MARGIN.left + PLOT_W)).toBeNull();

		expect(stepValues(s, 0)).toEqual({
			from: Date.parse('2026-11-04T18:00:00Z'),
			to: Date.parse('2026-11-04T18:01:00Z'),
			powerKw: 6,
			energyKwh: 0.1
		});
		expect(stepValues(s, 1).powerKw).toBeNull();
	});
});
