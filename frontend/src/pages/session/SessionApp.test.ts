import { flushSync } from 'svelte';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { SessionDetail, SessionPage, SessionPayments } from '$lib/contract.gen';
import { m } from '$lib/paraglide/messages';
import { setPageData } from '../../test/bootstrap';
import { stubFetch } from '../../test/fetch';
import { auditSession, paymentRecord } from '../../test/fixtures';
import { cleanup, render, textOf } from '../../test/render';
import SessionApp from './SessionApp.svelte';

vi.mock('$lib/bootstrap', () => import('../../test/bootstrap'));

afterEach(cleanup);

/** A session of a made-up pool: 6 kWh at 6 kW from 19:00 to 20:00, then plugged in until 20:40. */
function sessionDetail(overrides: Partial<SessionDetail> = {}): SessionDetail {
	return {
		id: 7,
		transaction_id: 4321,
		pool_code: 990001,
		cpo_name: 'Operador',
		station_name: 'ST1',
		connector_id: 2,
		card_alias: 'Pago QR',
		start_date: '04/11/2026',
		time_band: '19:00 - 20:40',
		stop_motive: 'CAR',
		started_at: '2026-11-04T19:00:00+01:00',
		ended_at: '2026-11-04T20:40:00+01:00',
		duration_seconds: 6000,
		energy_wh: 6000,
		cost: 2.6,
		bands: [
			{ band: 'off_peak', energy_wh: 0, rate: 0.25, cost: 0 },
			{ band: 'flat', energy_wh: 1000, rate: 0.35, cost: 0.35 },
			{ band: 'peak', energy_wh: 5000, rate: 0.45, cost: 2.25 }
		],
		evaluated_cost: 2.6,
		cost_check: 'ok',
		meter_value_count: 2,
		peak_power_w: 7200,
		mean_power_w: 6000,
		last_delivery_at: '2026-11-04T20:00:00+01:00',
		idle_seconds: 2400,
		telemetry_purged: false,
		series: { start: '2026-11-04T18:00:00Z', step: 60, power: [6000, 6000, null, 6000], energy: [100, 200, null, 300] },
		meter_values: [
			{ timestamp: '2026-11-04T19:01:00+01:00', meter_value: 50100, power: 6000, cost: 0.04, evaluated_cost: 0.035, band: 'Flat', off_peak_wh: 0, flat_wh: 100, peak_wh: 0 }, // prettier-ignore
			{ timestamp: '2026-11-04T20:00:00+01:00', meter_value: 56000, power: 6000, cost: 2.6, evaluated_cost: 2.6, band: 'Peak', off_peak_wh: 0, flat_wh: 1000, peak_wh: 5000 } // prettier-ignore
		],
		export_url: '/api/admin/audits/7/export',
		payments_url: '/api/admin/sessions/990001/ST1/2/4321/payments',
		audits_url: '/emsp-dashboard/audits/990001',
		...overrides
	};
}

const payments: SessionPayments = {
	payments: [paymentRecord({ id: 31, pool_code: 990001, station_name: 'ST1', connector_id: 2, amount: 260, order: 'ORDER-A' })],
	source: 'csms-1:8080'
};

function renderPage(page: SessionPage): HTMLElement {
	setPageData(page);
	return render(SessionApp, {});
}

async function settle(): Promise<void> {
	await new Promise((resolve) => setTimeout(resolve, 0));
	flushSync();
}

describe('SessionApp', () => {
	it('shows the session, its figures, bands, chart and payment', async () => {
		const fetch = stubFetch(() => ({ body: payments }));
		const target = renderPage({ transaction_id: 4321, session: sessionDetail(), matches: [] });
		await settle();

		expect(textOf(target.querySelector('h1'))).toBe(m.session_heading({ id: '4321' }));
		expect(textOf(target.querySelector('[data-stop-motive]'))).toBe(m.session_stop_car());
		expect(textOf(target.querySelector('[data-started]'))).toBe('04/11/2026, 19:00');
		const kpis = textOf(target.querySelector('[data-kpis]'));
		for (const value of ['6,000 kWh', '2,60 €', '1 h 40 min', '6,0 kW', '7,2 kW', '40 min', '0,4333 €/kWh']) {
			expect(kpis).toContain(value);
		}
		expect(target.querySelector('[data-cost-check="ok"]')).not.toBeNull();
		expect(target.querySelectorAll('[data-chart] [data-power-area]')).toHaveLength(2); // Broken by the gap
		expect(target.querySelectorAll('[data-chart] [data-energy-line]')).toHaveLength(2);

		expect(fetch).toHaveBeenCalledTimes(1);
		expect(fetch.mock.calls[0][0]).toBe('/api/admin/sessions/990001/ST1/2/4321/payments');
		const payment = target.querySelector('[data-payment="31"]');
		expect(textOf(payment?.querySelector('[data-status]'))).toBe(m.payments_status_committed());
		expect(textOf(payment)).toContain('2,60 €');
		expect(textOf(payment)).toContain('ORDER-A');
	});

	it('shows the meter values on demand', () => {
		stubFetch(() => ({ body: payments }));
		const target = renderPage({ transaction_id: 4321, session: sessionDetail(), matches: [] });

		expect(target.querySelector('[data-meter-values]')).toBeNull();
		target.querySelector<HTMLButtonElement>('[data-toggle-meter-values]')!.click();
		flushSync();
		const rows = target.querySelectorAll<HTMLTableRowElement>('[data-meter-values] tbody tr');
		expect(rows).toHaveLength(2);
		expect(textOf(rows[1].cells[0])).toBe('04/11, 20:00:00');
	});

	it('flags a capped cost and says when the session has no payment', async () => {
		stubFetch(() => ({ body: { payments: [], source: 'csms-1:8080' } satisfies SessionPayments }));
		const target = renderPage({
			transaction_id: 4321,
			session: sessionDetail({ cost: 2.5, stop_motive: 'COST', cost_check: 'capped' }),
			matches: []
		});
		await settle();

		expect(textOf(target.querySelector('[data-cost-check="capped"]'))).toBe(m.session_cost_capped());
		expect(textOf(target.querySelector('[data-no-payment]'))).toBe(m.session_payments_none());
	});

	it('shows why the payment is unknown when no CSMS answers', async () => {
		stubFetch(() => ({ status: 503, body: { code: 'service_unavailable', message: 'no CSMS answered' } }));
		const target = renderPage({ transaction_id: 4321, session: sessionDetail(), matches: [] });
		await settle();

		expect(textOf(target.querySelector('[data-payments] [role="alert"]'))).toBe(m.error_service_unavailable());
	});

	it('says when the meter values were purged', () => {
		stubFetch(() => ({ body: payments }));
		const target = renderPage({
			transaction_id: 4321,
			session: sessionDetail({ series: null, meter_values: [], meter_value_count: 0, telemetry_purged: true }),
			matches: []
		});

		expect(textOf(target.querySelector('[data-no-chart]'))).toBe(m.session_chart_purged());
		expect(target.querySelector('[data-toggle-meter-values]')).toBeNull();
	});

	it('tells apart a session without meter values from one without energy', () => {
		stubFetch(() => ({ body: payments }));
		const none = renderPage({
			transaction_id: 4321,
			session: sessionDetail({ series: null, meter_values: [], meter_value_count: 0 }),
			matches: []
		});
		expect(textOf(none.querySelector('[data-no-chart]'))).toBe(m.session_chart_no_meter_values());
		cleanup();

		const idle = renderPage({ transaction_id: 4321, session: sessionDetail({ series: null }), matches: [] });
		expect(textOf(idle.querySelector('[data-no-chart]'))).toBe(m.session_chart_none());
	});

	it('lists the sessions of a shared transaction id, without asking for payments', () => {
		const fetch = stubFetch(() => ({ body: payments }));
		const target = renderPage({
			transaction_id: 7783,
			session: null,
			matches: [
				auditSession({ transaction_id: 7783, station_name: 'EVC01' }),
				auditSession({ transaction_id: 7783, station_name: 'EVC01' }), // Two history rows of one session
				auditSession({ transaction_id: 7783, station_name: 'EVC07', session_url: '/emsp-dashboard/sessions/12/EVC07/1/7783' })
			]
		});

		const links = [...target.querySelectorAll<HTMLAnchorElement>('[data-matches] a')].map((a) => a.getAttribute('href'));
		expect(links).toEqual([
			'/emsp-dashboard/sessions/12/EVC01/1/7783',
			'/emsp-dashboard/sessions/12/EVC01/1/7783',
			'/emsp-dashboard/sessions/12/EVC07/1/7783'
		]);
		expect(fetch).not.toHaveBeenCalled();
	});

	it('says when no session has the transaction id', () => {
		stubFetch(() => ({ body: payments }));
		const target = renderPage({ transaction_id: 9999, session: null, matches: [] });
		expect(textOf(target.querySelector('[data-matches]'))).toContain(m.session_matches_empty({ id: '9999' }));
	});
});
