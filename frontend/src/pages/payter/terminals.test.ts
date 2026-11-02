import { describe, expect, it } from 'vitest';
import { payterTerminal as terminal } from '../../test/fixtures';
import { formStations, movedStations, otherSharedTerminal, poolGroups } from './terminals';

describe('formStations', () => {
	it('joins the ticked and the typed stations, without blanks or repeats', () => {
		expect(formStations(['ST-A', 'ST-B'], ' ST-C, ,ST-A ,ST-D ')).toEqual(['ST-A', 'ST-B', 'ST-C', 'ST-D']);
	});
});

describe('movedStations', () => {
	const terminals = [
		terminal({ serial_number: 'P6X01', linked_stations: ['ST-A', 'ST-B'] }),
		terminal({ serial_number: 'P6X02', linked_stations: ['ST-C'], is_active: false }),
		terminal({ serial_number: 'P6X03', pool_code: 241004, linked_stations: ['ST-D'] })
	];

	it('lists the stations taken from other active terminals of the same pool', () => {
		expect(movedStations(terminals, 'P6X09', 221006, ['ST-B', 'ST-C', 'ST-D'])).toEqual([{ station: 'ST-B', from: 'P6X01' }]);
	});

	it("ignores the edited terminal's own stations", () => {
		expect(movedStations(terminals, 'P6X01', 221006, ['ST-A'])).toEqual([]);
	});
});

describe('otherSharedTerminal', () => {
	const terminals = [terminal({ serial_number: 'P6X01', pool_code: 241004, is_pool_shared: true })];

	it('finds the active shared terminal of the pool', () => {
		expect(otherSharedTerminal(terminals, 'P6X02', 241004)?.serial_number).toBe('P6X01');
	});

	it('is none for the shared terminal itself or another pool', () => {
		expect(otherSharedTerminal(terminals, 'P6X01', 241004)).toBeUndefined();
		expect(otherSharedTerminal(terminals, 'P6X02', 221006)).toBeUndefined();
	});
});

describe('poolGroups', () => {
	it('groups by pool in code order, active terminals first', () => {
		const groups = poolGroups(
			[
				terminal({ serial_number: 'P6X03', pool_code: 241004 }),
				terminal({ serial_number: 'P6X01', is_active: false }),
				terminal({ serial_number: 'P6X02' })
			],
			[{ pool_code: 221006, cpo_name: 'Residencia', stations: [] }]
		);

		expect(groups.map((g) => [g.poolCode, g.cpoName, g.terminals.map((t) => t.serial_number)])).toEqual([
			[221006, 'Residencia', ['P6X02', 'P6X01']],
			[241004, '', ['P6X03']]
		]);
	});
});
