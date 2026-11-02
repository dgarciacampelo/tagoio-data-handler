// Rules of the Payter terminals form, kept out of the component so they can be tested on their own. The server
// applies the same ones (internal/handlers/payter.go); checking them here lets the page ask before moving stations.

import type { PayterPool, PayterTerminal } from '$lib/contract.gen';

/** A Payter P6x serial number, as the server and the CSMS validate it. */
export const serialPattern = /^P6X[0-9A-Z]+$/;

/** The stations of the form: the ticked ones plus the comma-separated extra ones, without blanks or repeats. */
export function formStations(selected: string[], extra: string): string[] {
	const names = [...selected, ...extra.split(',')].map((name) => name.trim()).filter((name) => name !== '');
	return [...new Set(names)];
}

/** A station that leaves another active terminal of the pool when the edited terminal is saved. */
export interface StationMove {
	station: string;
	from: string; // Serial number of its current terminal
}

export function movedStations(terminals: PayterTerminal[], serial: string, poolCode: number, stations: string[]): StationMove[] {
	return terminals
		.filter((t) => t.is_active && t.pool_code === poolCode && t.serial_number !== serial)
		.flatMap((t) => t.linked_stations.filter((station) => stations.includes(station)).map((station) => ({ station, from: t.serial_number })));
}

/** The active pool-shared terminal of a pool other than the edited one, if any: a pool has at most one. */
export function otherSharedTerminal(terminals: PayterTerminal[], serial: string, poolCode: number): PayterTerminal | undefined {
	return terminals.find((t) => t.is_active && t.is_pool_shared && t.pool_code === poolCode && t.serial_number !== serial);
}

export interface PoolGroup {
	poolCode: number;
	cpoName: string;
	terminals: PayterTerminal[]; // Active ones first
}

/** The terminals by pool, in pool code order. */
export function poolGroups(terminals: PayterTerminal[], pools: PayterPool[]): PoolGroup[] {
	const groups = new Map<number, PoolGroup>();
	for (const terminal of terminals) {
		let group = groups.get(terminal.pool_code);
		if (!group) {
			const cpoName = pools.find((p) => p.pool_code === terminal.pool_code)?.cpo_name ?? '';
			group = { poolCode: terminal.pool_code, cpoName, terminals: [] };
			groups.set(terminal.pool_code, group);
		}
		group.terminals.push(terminal);
	}
	for (const group of groups.values()) {
		group.terminals.sort((a, b) => Number(b.is_active) - Number(a.is_active) || a.serial_number.localeCompare(b.serial_number));
	}
	return [...groups.values()].sort((a, b) => a.poolCode - b.poolCode);
}
