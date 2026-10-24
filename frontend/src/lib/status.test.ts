import { describe, expect, it } from 'vitest';
import { statusStyle } from './status';
import type { ConnectorStatus } from './contract.gen';

// The labels and colors of the former templates (status-card.html and emsp-dashboard.html).
const expected: Record<ConnectorStatus, [string, string]> = {
	Available: ['DISPONIBLE', 'bg-status-available'],
	Preparing: ['PREPARANDO', 'bg-status-preparing'],
	Charging: ['CARGANDO', 'bg-status-charging'],
	SuspendedEV: ['SUSPENDIDO', 'bg-status-suspended'],
	SuspendedEVSE: ['SUSPENDIDO', 'bg-status-suspended'],
	Finishing: ['FINALIZANDO', 'bg-status-finishing'],
	Faulted: ['ERROR', 'bg-status-error'],
	Reserved: ['NO DISPONIBLE', 'bg-status-unavailable'],
	Unavailable: ['NO DISPONIBLE', 'bg-status-unavailable']
};

describe('statusStyle', () => {
	it.each(Object.entries(expected))('maps %s', (status, [label, background]) => {
		const style = statusStyle(status as ConnectorStatus);
		expect(style.label()).toBe(label);
		expect(style.background).toBe(background);
	});

	it('falls back to unavailable for an unknown status', () => {
		expect(statusStyle('Bogus' as ConnectorStatus).background).toBe('bg-status-unavailable');
	});
});
