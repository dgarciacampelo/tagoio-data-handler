import { afterEach, describe, expect, it, vi } from 'vitest';
import type { AuditsPage } from '$lib/contract.gen';
import { setPageData } from '../../test/bootstrap';
import { auditSession } from '../../test/fixtures';
import { cleanup, render, textOf } from '../../test/render';
import AuditsApp from './AuditsApp.svelte';

vi.mock('$lib/bootstrap', () => import('../../test/bootstrap'));

afterEach(cleanup);

/** The session rows of the table (each session has a second row with its tariff bands). */
function sessionRows(): { transaction: string; station: string }[] {
	return [...document.querySelectorAll<HTMLTableRowElement>('tbody tr')]
		.filter((row) => row.querySelector('a[href]')) // The XLSX export link
		.map((row) => ({ transaction: textOf(row.cells[0]), station: textOf(row.cells[1].firstElementChild) }));
}

describe('AuditsApp', () => {
	it('shows a row per session', () => {
		setPageData({
			pool_code: 0,
			sessions: [
				auditSession({ transaction_id: 7783, pool_code: 12, station_name: 'EVC01' }),
				auditSession({ transaction_id: 7790, pool_code: 12, station_name: 'EVC02' })
			]
		} satisfies AuditsPage);

		render(AuditsApp, {});

		expect(sessionRows()).toEqual([
			{ transaction: '7783', station: '12 / EVC01' },
			{ transaction: '7790', station: '12 / EVC02' }
		]);
	});

	it('shows both sessions that share a transaction id', () => {
		// Transaction ids are only unique per charge point: the local database already has transaction 7783 twice.
		setPageData({
			pool_code: 0,
			sessions: [
				auditSession({ transaction_id: 7783, pool_code: 12, station_name: 'EVC01' }),
				auditSession({ transaction_id: 7783, pool_code: 15, station_name: 'EVC07' })
			]
		} satisfies AuditsPage);

		render(AuditsApp, {});

		expect(sessionRows()).toEqual([
			{ transaction: '7783', station: '12 / EVC01' },
			{ transaction: '7783', station: '15 / EVC07' }
		]);
	});

	it('shows two history rows of the same session', () => {
		// The history's unique index includes last_meter_value, so a repeated final update with another reading
		// stores the session twice: both rows have the same pool, station, connector and transaction.
		setPageData({
			pool_code: 12,
			sessions: [
				auditSession({ transaction_id: 7783, pool_code: 12, station_name: 'EVC01' }),
				auditSession({ transaction_id: 7783, pool_code: 12, station_name: 'EVC01' })
			]
		} satisfies AuditsPage);

		render(AuditsApp, {});

		expect(sessionRows()).toEqual([
			{ transaction: '7783', station: '12 / EVC01' },
			{ transaction: '7783', station: '12 / EVC01' }
		]);
	});
});
