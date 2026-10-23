import { describe, expect, it } from 'vitest';
import { formatFixed, formatUpTo } from './format';

describe('formatFixed', () => {
	it.each([
		[12.345, 2, '12,35'],
		[7.4, 1, '7,4'],
		[0, 2, '0,00'],
		[2.5, 3, '2,500']
	])('formats %d with %d decimals as %s', (value, decimals, expected) => {
		expect(formatFixed(value, decimals)).toBe(expected);
	});
});

describe('formatUpTo', () => {
	it.each([
		[0.1845, 4, '0,1845'],
		[0.12, 4, '0,12'],
		[0.25, 4, '0,25'],
		[1, 4, '1']
	])('formats %d with up to %d decimals as %s', (value, decimals, expected) => {
		expect(formatUpTo(value, decimals)).toBe(expected);
	});
});
