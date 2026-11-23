import { describe, expect, it, vi } from 'vitest';
import { readEvents } from './client';
import { bandAt, formatLocalDateTime, formatMinutes, formatPower, sectionOf, sinceText } from './panel';
import { connectToTago, deviceOf, TagoError, type TagoSDK } from './tagoio';

describe('panel helpers', () => {
	it('maps the TagoIO tab hashes to sections', () => {
		expect(sectionOf('#tarifas')).toBe('tarifas');
		expect(sectionOf('#en-curso')).toBe('en-curso');
		expect(sectionOf('')).toBeUndefined();
		expect(sectionOf('#settings')).toBeUndefined();
	});

	it('formats for Spanish readers', () => {
		expect(formatPower(7360)).toBe('7,4 kW');
		expect(formatMinutes(125)).toBe('2 h 05 min');
		expect(formatMinutes(45)).toBe('45 min');
		expect(formatLocalDateTime('2026-11-20T12:17')).toBe('20/11/2026 12:17');
	});

	it('says how long ago something happened', () => {
		const now = Date.parse('2026-11-23T10:00:00Z');
		expect(sinceText(null, now)).toBe('');
		expect(sinceText('2026-11-23T09:59:30Z', now)).toBe('hace un momento');
		expect(sinceText('2026-11-23T09:45:00Z', now)).toBe('hace 15 min');
		expect(sinceText('2026-11-23T07:00:00Z', now)).toBe('hace 3 h');
		expect(sinceText('2026-11-20T10:00:00Z', now)).toBe('hace 3 días');
	});

	it('finds the band of an hour', () => {
		const bands = [
			{ band: 'off_peak' as const, from_hour: 0, to_hour: 8 },
			{ band: 'peak' as const, from_hour: 8, to_hour: 24 }
		];
		expect(bandAt(bands, 7)).toBe('off_peak');
		expect(bandAt(bands, 8)).toBe('peak');
	});
});

describe('TagoIO bridge', () => {
	const device = '637f4de7e7223300208aa890';

	/** A fake SDK whose callbacks the test fires. */
	function fakeSDK() {
		const handlers: { realtime?: (data: unknown) => void; user?: (info: { token: string | null }) => void } = {};
		const sdk: TagoSDK = {
			onRealtime: (cb) => (handlers.realtime = cb as (data: unknown) => void),
			onSyncUserInformation: (cb) => (handlers.user = cb),
			ready: vi.fn()
		};
		return { sdk, handlers };
	}

	it('takes the device from the realtime records, not the blueprint placeholder', () => {
		expect(deviceOf([{ result: [{ origin: 'placeholder' }, { device }] }])).toBe(device);
		expect(deviceOf([{ result: [] }])).toBeUndefined();
	});

	it('resolves once the token and the device arrived, and reports a later device change', async () => {
		const { sdk, handlers } = fakeSDK();
		const changed = vi.fn();
		const context = connectToTago(sdk, changed);
		expect(sdk.ready).toHaveBeenCalled();

		handlers.realtime?.([{ result: [{ device }] }]);
		handlers.user?.({ token: 'user-token' });
		await expect(context).resolves.toEqual({ token: 'user-token', deviceId: device });

		handlers.realtime?.([{ result: [{ device }] }]);
		expect(changed).not.toHaveBeenCalled();
		handlers.realtime?.([{ result: [{ device: '6304b5ee1ffc9500130e0b66' }] }]);
		expect(changed).toHaveBeenCalledWith('6304b5ee1ffc9500130e0b66');
	});

	it('gives up when TagoIO sends no token', async () => {
		vi.useFakeTimers();
		try {
			const { sdk, handlers } = fakeSDK();
			const context = connectToTago(sdk, vi.fn(), 1000);
			handlers.realtime?.([{ result: [{ device }] }]);
			handlers.user?.({ token: null });
			vi.advanceTimersByTime(1000);
			await expect(context).rejects.toBeInstanceOf(TagoError);
		} finally {
			vi.useRealTimers();
		}
	});
});

describe('readEvents', () => {
	it('reads the data of each event, across chunks, and skips the comments', async () => {
		const chunks = [': connected\n\ndata: {"a":', '1}\n\n: ping\n\ndata: {"a":2}\n\n'];
		const body = new ReadableStream<Uint8Array>({
			start(controller) {
				for (const chunk of chunks) controller.enqueue(new TextEncoder().encode(chunk));
				controller.close();
			}
		});
		const seen: string[] = [];
		await readEvents(new Response(body), (data) => seen.push(data));
		expect(seen).toEqual(['{"a":1}', '{"a":2}']);
	});
});
