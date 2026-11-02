import { flushSync } from 'svelte';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { PayterPage, PayterTerminal } from '$lib/contract.gen';
import { m } from '$lib/paraglide/messages';
import { setPageData } from '../../test/bootstrap';
import { jsonBody, stubFetch } from '../../test/fetch';
import { payterTerminal as terminal } from '../../test/fixtures';
import { cleanup, render, textOf } from '../../test/render';
import PayterApp from './PayterApp.svelte';

vi.mock('$lib/bootstrap', () => import('../../test/bootstrap'));

afterEach(cleanup);

const production: PayterTerminal[] = [
	terminal({ serial_number: 'P6X20222100772', linked_stations: ['22V20538CCPOS'] }),
	terminal({ serial_number: 'P6X20222101349', linked_stations: ['22V20537CCPOS'] }),
	terminal({ serial_number: 'P6X20231300653', pool_code: 241004, is_pool_shared: true })
];

function renderPage(terminals = production): HTMLElement {
	setPageData({
		terminals,
		pools: [
			{ pool_code: 221006, cpo_name: 'Residencia', stations: ['22V20537CCPOS', '22V20538CCPOS', '22V20539CCPOS'] },
			{ pool_code: 241004, cpo_name: '', stations: [] }
		]
	} satisfies PayterPage);
	return render(PayterApp, {});
}

function type(target: HTMLElement, selector: string, value: string): void {
	const input = target.querySelector<HTMLInputElement>(selector)!;
	input.value = value;
	input.dispatchEvent(new Event('input', { bubbles: true }));
	flushSync();
}

function tick(target: HTMLElement, station: string): void {
	target.querySelector<HTMLInputElement>(`input[type="checkbox"][value="${station}"]`)!.click();
	flushSync();
}

function submit(target: HTMLElement): void {
	target.querySelector('form')!.dispatchEvent(new SubmitEvent('submit', { bubbles: true, cancelable: true }));
	flushSync();
}

/** Lets the fake fetch answer and applies the DOM updates. */
async function settle(): Promise<void> {
	await new Promise((resolve) => setTimeout(resolve, 0));
	flushSync();
}

describe('PayterApp', () => {
	it('shows the terminals by pool, with the stations each one serves', () => {
		const target = renderPage();

		const served = (serial: string) => textOf(target.querySelector(`[data-terminal="${serial}"] [data-stations]`));
		expect([...target.querySelectorAll('[data-pool]')].map((s) => s.getAttribute('data-pool'))).toEqual(['221006', '241004']);
		expect(served('P6X20222101349')).toBe('22V20537CCPOS');
		expect(served('P6X20231300653')).toBe(m.payter_serves_all());
	});

	it('saves a terminal and shows the list the server answers', async () => {
		const saved = [...production, terminal({ serial_number: 'P6X20250000001', linked_stations: ['22V20539CCPOS'] })];
		const fetch = stubFetch(() => ({ body: saved }));
		const target = renderPage();

		type(target, '#payter-serial', 'p6x20250000001');
		type(target, '#payter-pool', '221006');
		tick(target, '22V20539CCPOS');
		submit(target);
		await settle();

		const [url, init] = fetch.mock.calls[0];
		expect(url).toBe('/api/admin/payter-terminals/P6X20250000001');
		expect(init?.method).toBe('PUT');
		expect(jsonBody(init)).toEqual({ pool_code: 221006, is_pool_shared: false, linked_stations: ['22V20539CCPOS'] });
		expect(target.querySelector('[data-terminal="P6X20250000001"]')).not.toBeNull();
		expect(textOf(target.querySelector('[role="status"]'))).toBe(m.payter_saved({ serial: 'P6X20250000001' }));
	});

	it('asks before taking a station from another terminal', () => {
		const confirm = vi.fn(() => false);
		vi.stubGlobal('confirm', confirm);
		const fetch = stubFetch(() => ({ body: production }));
		const target = renderPage();

		type(target, '#payter-serial', 'P6X20250000001');
		type(target, '#payter-pool', '221006');
		tick(target, '22V20537CCPOS');
		submit(target);

		expect(confirm).toHaveBeenCalledWith(m.payter_move_confirm({ moves: '22V20537CCPOS (P6X20222101349)' }));
		expect(fetch).not.toHaveBeenCalled();
	});

	it('needs a station unless the terminal is shared by the pool', () => {
		const fetch = stubFetch(() => ({ body: production }));
		const target = renderPage();

		type(target, '#payter-serial', 'P6X20250000001');
		type(target, '#payter-pool', '221006');
		submit(target);

		expect(textOf(target.querySelector('[role="alert"]'))).toBe(m.payter_no_stations());
		expect(fetch).not.toHaveBeenCalled();
	});

	it('deactivates a terminal after confirming', async () => {
		vi.stubGlobal('confirm', vi.fn(() => true));
		const answer = production.map((t) => (t.serial_number === 'P6X20231300653' ? { ...t, is_active: false } : t));
		const fetch = stubFetch(() => ({ body: answer }));
		const target = renderPage();

		const card = target.querySelector('[data-terminal="P6X20231300653"]')!;
		[...card.querySelectorAll('button')].find((b) => textOf(b) === m.payter_deactivate())!.click();
		await settle();

		expect(fetch.mock.calls[0][0]).toBe('/api/admin/payter-terminals/P6X20231300653');
		expect(fetch.mock.calls[0][1]?.method).toBe('DELETE');
		expect(textOf(target.querySelector('[data-terminal="P6X20231300653"]'))).toContain(m.payter_inactive());
	});
});
