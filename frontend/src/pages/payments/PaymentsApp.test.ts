import { flushSync } from 'svelte';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { PaymentsPage, PaymentsResult } from '$lib/contract.gen';
import { m } from '$lib/paraglide/messages';
import { setPageData } from '../../test/bootstrap';
import { stubFetch } from '../../test/fetch';
import { cleanup, render, textOf } from '../../test/render';
import PaymentsApp from './PaymentsApp.svelte';
import { paymentRecord as payment } from '../../test/fixtures';

vi.mock('$lib/bootstrap', () => import('../../test/bootstrap'));

afterEach(cleanup);

const page: PaymentsPage = {
	configured: true,
	today: '2026-11-05',
	pools: [{ pool_code: 221006, cpo_name: 'Residencia' }]
};

const answer: PaymentsResult = {
	payments: [
		payment({ id: 12, status: 'ERROR', amount: null, transaction_id: null, error_stage: 'PREAUTH', error_code: 562,
			error_message: 'Paycomet refused the preauth', error_at: '2026-11-05T10:00:02+00:00' }), // prettier-ignore
		payment({ id: 11 })
	],
	truncated: false,
	source: 'csms-1:8080',
	unanswered: []
};

function renderPage(view: PaymentsPage = page): HTMLElement {
	setPageData(view);
	return render(PaymentsApp, {});
}

async function settle(): Promise<void> {
	await new Promise((resolve) => setTimeout(resolve, 0));
	flushSync();
}

describe('PaymentsApp', () => {
	it('loads the last seven days and shows each payment with its error', async () => {
		const fetch = stubFetch(() => ({ body: answer }));
		const target = renderPage();
		await settle();

		expect(fetch.mock.calls[0][0]).toBe('/api/admin/payments?from=2026-10-30&to=2026-11-05');
		const row = (id: number) => target.querySelector(`[data-payment="${id}"]`);
		expect(textOf(row(12)?.querySelector('[data-status]') ?? null)).toBe(m.payments_status_error());
		expect(textOf(row(12)?.querySelector('[data-error]') ?? null)).toContain('PREAUTH 562: Paycomet refused the preauth');
		expect(textOf(row(11)?.querySelector('[data-status]') ?? null)).toBe(m.payments_status_committed());
		expect(textOf(target.querySelector('[data-committed]'))).toBe(m.payments_committed_total({ total: '12,34 €' }));
	});

	it('filters by status from the summary', async () => {
		stubFetch(() => ({ body: answer }));
		const target = renderPage();
		await settle();

		target.querySelector<HTMLButtonElement>('[data-status-filter="ERROR"]')!.click();
		flushSync();

		expect([...target.querySelectorAll('[data-payment]')].map((r) => r.getAttribute('data-payment'))).toEqual(['12']);
	});

	it('asks for the chosen pool', async () => {
		const fetch = stubFetch(() => ({ body: answer }));
		const target = renderPage();
		await settle();

		const select = target.querySelector<HTMLSelectElement>('#payments-pool')!;
		select.selectedIndex = [...select.options].findIndex((o) => o.value === '221006');
		select.dispatchEvent(new Event('change', { bubbles: true }));
		flushSync();
		target.querySelector('form')!.dispatchEvent(new SubmitEvent('submit', { bubbles: true, cancelable: true }));
		await settle();

		expect(fetch.mock.calls[1][0]).toBe('/api/admin/payments?from=2026-10-30&to=2026-11-05&pool_code=221006');
	});

	it('explains a CSMS that does not answer', async () => {
		stubFetch(() => ({ status: 503, body: { code: 'service_unavailable', message: 'no CSMS answered' } }));
		const target = renderPage();
		await settle();

		expect(textOf(target.querySelector('[role="alert"]'))).toBe(m.error_service_unavailable());
	});

	it('asks nothing when no CSMS is configured', () => {
		const fetch = stubFetch(() => ({ body: answer }));
		const target = renderPage({ ...page, configured: false });

		expect(textOf(target.querySelector('[role="alert"]'))).toBe(m.payments_not_configured());
		expect(fetch).not.toHaveBeenCalled();
	});

	it("links a payment's transaction to its session page", async () => {
		stubFetch(() => ({
			body: {
				...answer,
				payments: [
					payment({ id: 11, pool_code: 990001, station_name: 'DEMO 01' }),
					payment({ id: 13, pool_code: 990001, station_name: 'DEMO-02', connector_id: null, transaction_id: 4400, platform: 'PAYTER' })
				]
			} satisfies PaymentsResult
		}));
		const target = renderPage();
		await settle();

		const link = (id: number) => target.querySelector(`[data-payment="${id}"] [data-session-link]`)?.getAttribute('href');
		expect(link(11)).toBe('/emsp-dashboard/sessions/990001/DEMO%2001/1/4321'); // The station name escaped
		expect(link(13)).toBe('/emsp-dashboard/sessions/4400'); // No connector: found by the transaction id alone
	});
});
