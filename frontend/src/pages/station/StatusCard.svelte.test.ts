// A .svelte.test.ts file, so that $state can drive the props like StationApp's polling does.

import { flushSync } from 'svelte';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { StationLive } from '$lib/contract.gen';
import { m } from '$lib/paraglide/messages';
import { jsonBody, stubFetch } from '../../test/fetch';
import { stationLive } from '../../test/fixtures';
import { knownBug } from '../../test/knownBug';
import { cleanup, render, textOf } from '../../test/render';
import StatusCard from './StatusCard.svelte';

afterEach(cleanup);

const paidSession = stationLive({ status: 'Charging', panel: 'none', session_card: 'stop', show_metrics: true });

/** Renders the card of connector 1 of station EVC01 (pool 12); assigning props.live works like a new poll. */
function renderCard(live: StationLive) {
	const props = $state({ live, poolCode: 12, stationName: 'EVC01', connectorId: 1 });
	const target = render(StatusCard, props);
	return { props, stopButton: () => target.querySelector('button') };
}

/** Clicks the stop button and waits for the server's answer. */
async function stopSession(button: HTMLButtonElement) {
	button.click();
	await vi.waitFor(() => expect(textOf(button)).toBe(m.station_stop_sent()));
}

describe('StatusCard stop button', () => {
	it('is shown, enabled, during a paid session', () => {
		const { stopButton } = renderCard(paidSession);

		const button = stopButton();
		expect(button).not.toBeNull();
		expect(button!.disabled).toBe(false);
		expect(textOf(button)).toBe(m.station_stop_button());
	});

	it('is not shown without a session', () => {
		const { stopButton } = renderCard(stationLive({ status: 'Available', session_card: 'none' }));

		expect(stopButton()).toBeNull();
	});

	it('sends the stop request and shows it was sent', async () => {
		const fetch = stubFetch(() => ({ status: 202 }));
		const { stopButton } = renderCard(paidSession);

		await stopSession(stopButton()!);

		expect(fetch).toHaveBeenCalledOnce();
		const [url, init] = fetch.mock.calls[0];
		expect(url).toBe('/api/stop-request');
		expect(init?.method).toBe('POST');
		expect(jsonBody(init)).toEqual({ pool_code: 12, station_name: 'EVC01', connector_id: 1 });
		expect(stopButton()!.disabled).toBe(true); // No second request for the same session
	});

	it('is enabled again for the next paid session', async (context) => {
		stubFetch(() => ({ status: 202 }));
		const { props, stopButton } = renderCard(paidSession);
		await stopSession(stopButton()!);

		// The session ends, then the next driver pays and starts a new one.
		props.live = stationLive({ status: 'Available', session_card: 'none' });
		flushSync();
		expect(stopButton()).toBeNull();
		props.live = paidSession;
		flushSync();
		const button = stopButton();
		expect(button).not.toBeNull();

		await knownBug(
			context,
			'StatusCard keeps its stop state "sent" after the session ends, so the stop button stays disabled in the later paid sessions',
			() => {
				expect(button!.disabled, 'stop button disabled').toBe(false);
				expect(textOf(button)).toBe(m.station_stop_button());
			}
		);
	});
});
