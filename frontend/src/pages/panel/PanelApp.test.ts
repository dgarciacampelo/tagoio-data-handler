import { flushSync } from 'svelte';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { m } from '$lib/paraglide/messages';
import type { PanelLive, PanelOverview, PanelSession } from '$lib/contract.gen';
import { cleanup, render, textOf } from '../../test/render';
import PanelApp from './PanelApp.svelte';
import type { TagoSDK } from './tagoio';

const device = '637f4de7e7223300208aa890';

const session: PanelSession = { session: 'signed', expires_at: '2026-11-23T11:00:00Z', pool_code: 220001, user_kind: 'run_user' };

const overview: PanelOverview = {
	pool_code: 220001,
	installation_name: 'MASTER-BUSINESS-220001',
	cpo: { name: 'Velo', fiscal_id: 'B1', address: '', phone: '', email: '', web: '' },
	rates: { off_peak: 0.18, flat: 0.27, peak: 0.39, vat_percent: 21, withholding: 40 },
	max_power_w: 30000,
	load_balancing_mode: 'Dinámico normal',
	balancing_modes: ['Ninguno', 'Estático normal', 'Dinámico normal', 'Dinámico boost'],
	stations: ['ST1'],
	bands: [{ band: 'off_peak', from_hour: 0, to_hour: 24 }]
};

const live: PanelLive = {
	generated_at: '2026-11-23T10:00:00Z',
	stations: [{ name: 'ST1', connection: 'Online', connection_since: null, connectors: [{ id: 1, status: 'Charging', since: null }] }],
	sessions: [],
	total_power_w: 7200
};

/** A TagoIO SDK that sends the token and the device at once. */
function fakeSDK(token = 'user-token'): TagoSDK {
	return {
		onRealtime: (cb) => queueMicrotask(() => cb([{ result: [{ device }] }])),
		onSyncUserInformation: (cb) => queueMicrotask(() => cb({ token })),
		ready: () => {}
	};
}

/** The panel APIs: a session, the overview, the history and a stream with one state. */
function stubPanelAPIs(sessionStatus = 200) {
	const fetch = vi.fn(async (input: RequestInfo | URL, init: RequestInit = {}) => {
		const url = String(input);
		const json = (body: unknown, status = 200) =>
			new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });
		if (url === '/api/panel/session') {
			return sessionStatus === 200 ? json(session) : json({ code: 'forbidden', message: 'no' }, sessionStatus);
		}
		if (url.endsWith('/overview')) return json(overview);
		if (url.includes('/history')) return json({ items: [], total: 0, page: 1, page_size: 25, totals: { sessions: 0, minutes: 0, energy_kwh: 0, cost: 0 } });
		if (url.endsWith('/stream')) {
			const body = new ReadableStream<Uint8Array>({
				start(controller) {
					controller.enqueue(new TextEncoder().encode(`data: ${JSON.stringify(live)}\n\n`));
					init.signal?.addEventListener('abort', () => controller.close());
				}
			});
			return new Response(body, { headers: { 'Content-Type': 'text/event-stream' } });
		}
		return json({ code: 'not_found', message: url }, 404);
	});
	vi.stubGlobal('fetch', fetch);
	return fetch;
}

async function settle() {
	for (let i = 0; i < 10; i++) {
		await new Promise((resolve) => setTimeout(resolve, 0));
	}
	flushSync();
}

afterEach(() => {
	cleanup();
	window.location.hash = '';
});

describe('PanelApp', () => {
	it('starts a session with the TagoIO token and shows the live stations', async () => {
		const fetch = stubPanelAPIs();
		render(PanelApp, { sdk: fakeSDK() });
		await settle();

		const [, init] = fetch.mock.calls.find(([url]) => url === '/api/panel/session')!;
		expect(JSON.parse(String(init?.body))).toEqual({ token: 'user-token', device_id: device });
		const overviewCall = fetch.mock.calls.find(([url]) => String(url).endsWith('/overview'))!;
		expect((overviewCall[1]?.headers as Record<string, string>).Authorization).toBe('Bearer signed');

		const text = textOf(document.body);
		expect(text).toContain('ST1');
		expect(text).toContain(m.panel_connection_online());
		expect(text).toContain('Dinámico normal');
		expect(text).toContain('7,2 kW');
	});

	it('shows only the section of the TagoIO tab', async () => {
		stubPanelAPIs();
		window.location.hash = '#cpo';
		render(PanelApp, { sdk: fakeSDK() });
		await settle();

		expect(document.querySelector('nav')).toBeNull();
		expect(textOf(document.body)).toContain(m.panel_cpo_title());
		expect(textOf(document.body)).not.toContain(m.panel_stations_title());
	});

	it('explains when the user may not manage the installation', async () => {
		stubPanelAPIs(403);
		render(PanelApp, { sdk: fakeSDK() });
		await settle();

		expect(textOf(document.querySelector('[role=alert]'))).toContain(m.error_forbidden());
	});
});
