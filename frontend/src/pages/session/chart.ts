// Geometry of the power and energy chart of a session: the same drawing as the chart of its email (neos-mailer-mqtt
// internal/chart), as SVG. The mean power of each step is a green step area (kW, left scale), the delivered energy a
// blue line (kWh, right scale); a step without meter values (a gap) breaks both. The time axis is in the pools' time
// zone.

import type { SessionSeries } from '$lib/contract.gen';
import { formatFixed } from '$lib/format';

export const WIDTH = 600;
export const HEIGHT = 300;
export const MARGIN = { left: 52, right: 56, top: 12, bottom: 30 };
export const PLOT_W = WIDTH - MARGIN.left - MARGIN.right;
export const PLOT_H = HEIGHT - MARGIN.top - MARGIN.bottom;
export const GRID_LINES = 4;
export const TIME_ZONE = 'Europe/Madrid'; // Every pool's

const MAX_TIME_TICKS = 6;
const MINUTE = 60_000;
/** The time ticks to choose from: the first that gives at most MAX_TIME_TICKS ticks (all divide a day). */
const TICK_STEPS = [5, 10, 15, 30, 60, 120, 180, 240, 360].map((minutes) => minutes * MINUTE);

/**
 * Rounds a maximum up to a top for GRID_LINES even gridlines, each 1, 1.5, 2, 2.5, 3, 4, 5, 6 or 8 times a power of
 * ten (11 kW gets 12, not 20), as the email does.
 */
export function niceMax(v: number): number {
	if (!(v > 0)) return 1;
	const exp = 10 ** Math.floor(Math.log10(v / GRID_LINES));
	for (const m of [1, 1.5, 2, 2.5, 3, 4, 5, 6, 8, 10]) {
		const top = m * exp * GRID_LINES;
		if (top >= v * (1 - 1e-9)) return top;
	}
	return v;
}

/** How many decimals show a gridline step exactly (0,25 needs 2), up to 3. */
export function decimals(step: number): number {
	for (let d = 0; d < 3; d++) {
		const scaled = step * 10 ** d;
		if (Math.abs(scaled - Math.round(scaled)) < 1e-9) return d;
	}
	return 3;
}

export interface Gridline {
	y: number;
	power: string; // kW, left
	energy: string; // kWh, right
}

export interface TimeTick {
	x: number;
	label: string; // "HH:MM" in the pool's time zone
}

export interface ChartGeometry {
	steps: number;
	gridlines: Gridline[];
	ticks: TimeTick[];
	powerAreas: string[]; // SVG path data, one per run of steps with values
	powerLines: string[];
	energyLines: string[];
}

const hourMinute = new Intl.DateTimeFormat('es-ES', { timeZone: TIME_ZONE, hour: '2-digit', minute: '2-digit', hourCycle: 'h23' });

/** The time of day in the pool's time zone, "19:05". */
export function formatHourMinute(instant: number): string {
	return hourMinute.format(instant);
}

const wallClock = new Intl.DateTimeFormat('en-US', {
	timeZone: TIME_ZONE,
	hourCycle: 'h23',
	year: 'numeric',
	month: 'numeric',
	day: 'numeric',
	hour: 'numeric',
	minute: 'numeric',
	second: 'numeric'
});

/** The pool's UTC offset at an instant, in ms (Madrid: one or two hours). */
export function utcOffset(instant: number): number {
	const parts = Object.fromEntries(wallClock.formatToParts(instant).map((p) => [p.type, p.value]));
	const wall = Date.UTC(+parts.year, +parts.month - 1, +parts.day, +parts.hour, +parts.minute, +parts.second);
	return wall - Math.floor(instant / 1000) * 1000;
}

/** The start of the series and the length of its steps, in ms. */
export function timing(series: SessionSeries): { start: number; step: number; end: number } {
	const start = Date.parse(series.start);
	const step = series.step * 1000;
	return { start, step, end: start + step * series.power.length };
}

/** The x of a moment, given in steps from the start of the series. */
function xOf(steps: number, n: number): number {
	return MARGIN.left + (steps / n) * PLOT_W;
}

function yOf(value: number, top: number): number {
	return MARGIN.top + PLOT_H - Math.min(Math.max(value / top, 0), 1) * PLOT_H;
}

function path(points: [number, number][], close = false): string {
	const d = points.map(([x, y], i) => `${i === 0 ? 'M' : 'L'}${round(x)} ${round(y)}`).join(' ');
	return close ? `${d} Z` : d;
}

function round(v: number): number {
	return Math.round(v * 100) / 100;
}

/** Lays out the chart of a series. */
export function chartGeometry(series: SessionSeries): ChartGeometry {
	const n = series.power.length;
	const { start, step, end } = timing(series);
	const stepHours = series.step / 3600;

	let maxPower = 0;
	let maxEnergy = 0;
	for (let i = 0; i < n; i++) {
		maxPower = Math.max(maxPower, (series.power[i] ?? 0) / 1000);
		maxEnergy = Math.max(maxEnergy, (series.energy[i] ?? 0) / 1000);
	}
	const powerTop = niceMax(maxPower);
	const energyTop = niceMax(maxEnergy);

	const powerDigits = decimals(powerTop / GRID_LINES);
	const energyDigits = decimals(energyTop / GRID_LINES);
	const gridlines: Gridline[] = [];
	for (let g = 0; g <= GRID_LINES; g++) {
		gridlines.push({
			y: MARGIN.top + (PLOT_H * (GRID_LINES - g)) / GRID_LINES,
			power: formatFixed((powerTop * g) / GRID_LINES, powerDigits),
			energy: formatFixed((energyTop * g) / GRID_LINES, energyDigits)
		});
	}

	// Ticks on round local times, counted from the local midnight (3 h ticks fall on 00:00, 03:00...), after the start
	const tick = TICK_STEPS.find((t) => (end - start) / t <= MAX_TIME_TICKS) ?? TICK_STEPS[TICK_STEPS.length - 1];
	const offset = utcOffset(start);
	const ticks: TimeTick[] = [];
	for (let t = (Math.floor((start + offset) / tick) + 1) * tick - offset; t < end; t += tick) {
		ticks.push({ x: round(xOf((t - start) / step, n)), label: formatHourMinute(t) });
	}

	// Power: a step area (and its outline) for each run of steps with values
	const powerAreas: string[] = [];
	const powerLines: string[] = [];
	let run: [number, number][] = [];
	const flush = () => {
		if (run.length > 0) {
			const base = yOf(0, powerTop);
			powerAreas.push(path([[run[0][0], base], ...run, [run[run.length - 1][0], base]], true));
			powerLines.push(path(run));
		}
		run = [];
	};
	for (let i = 0; i < n; i++) {
		const watts = series.power[i];
		if (watts === null) {
			flush();
			continue;
		}
		const y = yOf(watts / 1000, powerTop);
		run.push([xOf(i, n), y], [xOf(i + 1, n), y]);
	}
	flush();

	// Energy: a line for each run of steps with values, from the energy at the start of its first step
	const energyLines: string[] = [];
	let line: [number, number][] = [];
	for (let i = 0; i < n; i++) {
		const wh = series.energy[i];
		if (wh === null) {
			if (line.length > 0) energyLines.push(path(line));
			line = [];
			continue;
		}
		const watts = series.power[i];
		if (line.length === 0 && watts !== null) {
			const before = Math.max(wh - watts * stepHours, 0);
			line.push([xOf(i, n), yOf(before / 1000, energyTop)]);
		}
		line.push([xOf(i + 1, n), yOf(wh / 1000, energyTop)]);
	}
	if (line.length > 0) energyLines.push(path(line));

	return { steps: n, gridlines, ticks, powerAreas, powerLines, energyLines };
}

/** The step under an x of the chart (its own coordinates), or null outside the plot. */
export function stepAt(series: SessionSeries, x: number): number | null {
	const n = series.power.length;
	const i = Math.floor(((x - MARGIN.left) / PLOT_W) * n);
	return i >= 0 && i < n ? i : null;
}

/** The x range of a step, for its highlight. */
export function stepBounds(series: SessionSeries, i: number): { x: number; width: number } {
	const n = series.power.length;
	return { x: round(xOf(i, n)), width: round(PLOT_W / n) };
}

export interface StepValues {
	from: number; // ms
	to: number;
	powerKw: number | null;
	energyKwh: number | null;
}

/** What a step shows: its time range, mean power and the energy delivered by its end. */
export function stepValues(series: SessionSeries, i: number): StepValues {
	const { start, step } = timing(series);
	const power = series.power[i];
	const energy = series.energy[i];
	return {
		from: start + i * step,
		to: start + (i + 1) * step,
		powerKw: power === null ? null : power / 1000,
		energyKwh: energy === null ? null : energy / 1000
	};
}
