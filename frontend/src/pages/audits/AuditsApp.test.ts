import { afterEach, describe, expect, it, vi } from 'vitest';
import type { AuditsPage } from '$lib/contract.gen';
import { setPageData } from '../../test/bootstrap';
import { auditSession } from '../../test/fixtures';
import { knownBug } from '../../test/knownBug';
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

	it('shows both sessions that share a transaction id', async (context) => {
		// Transaction ids are only unique per charge point: the local database already has transaction 7783 twice.
		setPageData({
			pool_code: 0,
			sessions: [
				auditSession({ transaction_id: 7783, pool_code: 12, station_name: 'EVC01' }),
				auditSession({ transaction_id: 7783, pool_code: 15, station_name: 'EVC07' })
			]
		} satisfies AuditsPage);

		let mountError: unknown;
		try {
			render(AuditsApp, {});
		} catch (error) {
			// Any other failure is not this bug: let it fail the test.
			if (!String(error).includes('each_key_duplicate')) throw error;
			mountError = error;
		}

		await knownBug(
			context,
			'AuditsApp keys its {#each} by transaction_id, which is not unique: Svelte throws each_key_duplicate and the page does not render',
			() => {
				expect(mountError, 'rendering must not throw').toBeUndefined();
				expect(sessionRows()).toEqual([
					{ transaction: '7783', station: '12 / EVC01' },
					{ transaction: '7783', station: '15 / EVC07' }
				]);
			}
		);
	});
});
