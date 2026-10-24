// Fake of the global fetch used by $lib/api, so the tests go through the real apiFetch (headers, JSON, AbortSignal).

import { vi } from 'vitest';

export interface FakeReply {
	status?: number; // 200 by default
	body?: unknown; // Sent as JSON
	delay?: number; // ms before the answer (setTimeout, so fake timers control it); none: answers at once
}

/**
 * Replaces the global fetch with a mock answering each request with reply(url, init). Like the real fetch, a
 * request whose signal is aborted before the answer rejects with an AbortError. vi.unstubAllGlobals() restores it
 * (unstubGlobals is on in vitest.config.ts).
 */
export function stubFetch(reply: (url: string, init: RequestInit) => FakeReply) {
	const fetch = vi.fn((input: RequestInfo | URL, init: RequestInit = {}): Promise<Response> => {
		const { status = 200, body, delay } = reply(String(input), init);
		const response = () =>
			new Response(body === undefined ? null : JSON.stringify(body), {
				status,
				headers: { 'Content-Type': 'application/json' }
			});
		if (delay === undefined) {
			return Promise.resolve(response());
		}

		const { signal } = init;
		return new Promise<Response>((resolve, reject) => {
			const aborted = () => signal?.reason ?? new DOMException('The operation was aborted.', 'AbortError');
			if (signal?.aborted) {
				reject(aborted());
				return;
			}
			const onAbort = () => {
				clearTimeout(timer);
				reject(aborted());
			};
			const timer = setTimeout(() => {
				signal?.removeEventListener('abort', onAbort);
				resolve(response());
			}, delay);
			signal?.addEventListener('abort', onAbort, { once: true });
		});
	});
	vi.stubGlobal('fetch', fetch);
	return fetch;
}

/** The parsed JSON body of a request sent through the fake fetch. */
export function jsonBody(init: RequestInit | undefined): unknown {
	return JSON.parse(String(init?.body));
}
