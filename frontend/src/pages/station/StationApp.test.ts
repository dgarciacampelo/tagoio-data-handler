import { flushSync } from 'svelte';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { m } from '$lib/paraglide/messages';
import { setPageData } from '../../test/bootstrap';
import { stubFetch } from '../../test/fetch';
import { stationLive, stationPage } from '../../test/fixtures';
import { knownBug } from '../../test/knownBug';
import { cleanup, render, textOf } from '../../test/render';
import StationApp from './StationApp.svelte';

vi.mock('$lib/bootstrap', () => import('../../test/bootstrap'));

const pollInterval = 5000; // StationApp's
const page = stationPage(stationLive({ status: 'Available', panel: 'plug_in' }));
// The driver plugs the vehicle in: the next poll reports the connector as Preparing, with the payment form.
const preparing = stationLive({ status: 'Preparing', panel: 'payment_form' });

/** The status label of the connector (StatusCard's heading). */
function statusLabel(): string {
	return textOf(document.querySelector('main h2')?.previousElementSibling);
}

/** Lets the fake clock run, with the promise callbacks (fetch, JSON) in between, and applies the DOM updates. */
async function elapse(ms: number): Promise<void> {
	await vi.advanceTimersByTimeAsync(ms);
	flushSync();
}

beforeEach(() => {
	vi.useFakeTimers();
	setPageData(page);
});

afterEach(() => {
	cleanup();
	vi.useRealTimers();
});

describe('StationApp polling', () => {
	it('shows the polled status when the answer is faster than the poll interval', async () => {
		const fetch = stubFetch(() => ({ body: preparing, delay: 300 }));
		render(StationApp, {});
		expect(statusLabel()).toBe(m.status_available());

		await elapse(pollInterval + 300);

		expect(fetch).toHaveBeenCalledOnce();
		expect(fetch).toHaveBeenCalledWith(page.poll_url, expect.objectContaining({ method: 'GET' }));
		expect(statusLabel()).toBe(m.status_preparing());
		expect(textOf(document.querySelector('main'))).toContain(m.station_payment_title());
	});

	it('shows the polled status when the answer is slower than the poll interval', async (context) => {
		const fetch = stubFetch(() => ({ body: preparing, delay: pollInterval + 2000 }));
		render(StationApp, {});
		expect(statusLabel()).toBe(m.status_available());

		await elapse(6 * pollInterval);

		// The page is polling (a broken setup must fail, not pass for the bug).
		expect(fetch).toHaveBeenCalled();
		for (const [url, init] of fetch.mock.calls) {
			expect(url).toBe(page.poll_url);
			expect(init?.method).toBe('GET');
		}

		await knownBug(
			context,
			'StationApp aborts the poll in flight on every 5 s tick, so an answer slower than 5 s never updates the page',
			() => {
				expect(statusLabel(), 'status after 30 s of answers that take 7 s').toBe(m.status_preparing());
			}
		);
	});
});
