// Client of the panel APIs: it starts a panel session with the TagoIO token, sends it on every call, starts a new
// one when it expires, and keeps the live stream open.

import { ApiError } from '$lib/api';
import type { APIError, PanelLive, PanelSession } from '$lib/contract.gen';
import type { TagoContext } from './tagoio';

type Method = 'GET' | 'POST' | 'PUT' | 'DELETE';

export class PanelClient {
	private session: PanelSession | undefined;
	private starting: Promise<PanelSession> | undefined;

	constructor(private readonly tago: TagoContext) {}

	get deviceId(): string {
		return this.tago.deviceId;
	}

	/** Starts a panel session (again, when force is set). */
	async start(force = false): Promise<PanelSession> {
		if (this.session && !force) return this.session;
		this.starting ??= (async () => {
			try {
				const response = await send('/api/panel/session', {
					method: 'POST',
					headers: { 'Content-Type': 'application/json' },
					body: JSON.stringify({ token: this.tago.token, device_id: this.tago.deviceId })
				});
				this.session = (await response.json()) as PanelSession;
				return this.session;
			} finally {
				this.starting = undefined;
			}
		})();
		return this.starting;
	}

	/** Calls a panel API of the device (path after /api/panel/{device}). Resolves with its JSON, or undefined. */
	async call<T = void>(method: Method, path: string, body?: unknown): Promise<T> {
		const response = await this.authorized(path, {
			method,
			headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
			body: body === undefined ? undefined : JSON.stringify(body)
		});
		if (response.status === 204) return undefined as T;
		return (await response.json()) as T;
	}

	/** Downloads a file of the panel APIs (the history CSV), with its server file name. */
	async download(path: string): Promise<{ blob: Blob; name: string }> {
		const response = await this.authorized(path, { method: 'GET' });
		const disposition = response.headers.get('Content-Disposition') ?? '';
		const name = /filename="([^"]+)"/.exec(disposition)?.[1] ?? 'export.csv';
		return { blob: await response.blob(), name };
	}

	/**
	 * Keeps the live stream open, calling onLive with every state and onConnected when the stream connects or
	 * drops. Returns a function that closes it.
	 */
	stream(onLive: (live: PanelLive) => void, onConnected: (connected: boolean) => void): () => void {
		const controller = new AbortController();
		const run = async () => {
			let delay = 1000;
			while (!controller.signal.aborted) {
				try {
					const response = await this.authorized('/stream', { method: 'GET', signal: controller.signal });
					onConnected(true);
					delay = 1000;
					await readEvents(response, (data) => onLive(JSON.parse(data) as PanelLive));
				} catch (error) {
					if (controller.signal.aborted) return;
					if (error instanceof ApiError && (error.status === 401 || error.status === 403)) {
						this.session = undefined;
					}
				}
				onConnected(false);
				if (controller.signal.aborted) return;
				await sleep(delay, controller.signal);
				delay = Math.min(delay * 2, 30000);
			}
		};
		void run();
		return () => controller.abort();
	}

	/** Sends a request with the session, starting a new session once when the server says it ended. */
	private async authorized(path: string, init: RequestInit): Promise<Response> {
		const url = `/api/panel/${this.tago.deviceId}${path}`;
		for (let attempt = 0; ; attempt++) {
			const session = await this.start();
			try {
				return await send(url, { ...init, headers: { ...init.headers, Authorization: `Bearer ${session.session}` } });
			} catch (error) {
				if (attempt === 0 && error instanceof ApiError && error.status === 401) {
					this.session = undefined;
					continue;
				}
				throw error;
			}
		}
	}
}

/** fetch that throws ApiError for a failed answer, like $lib/api's apiFetch (without cookies: the panel has none). */
async function send(url: string, init: RequestInit): Promise<Response> {
	let response: Response;
	try {
		response = await fetch(url, { ...init, credentials: 'omit' });
	} catch (error) {
		if (init.signal?.aborted) throw error;
		throw new ApiError(0, undefined, `Network error: ${String(error)}`);
	}
	if (!response.ok) {
		const error = (await response.json().catch(() => undefined)) as APIError | undefined;
		throw new ApiError(response.status, error?.code, error?.message ?? response.statusText);
	}
	return response;
}

/** Reads a Server-Sent Events body, calling onData with the data of every event, until it ends. */
export async function readEvents(response: Response, onData: (data: string) => void): Promise<void> {
	const reader = response.body?.getReader();
	if (!reader) return;
	const decoder = new TextDecoder();
	let buffer = '';
	for (;;) {
		const { value, done } = await reader.read();
		if (done) return;
		buffer += decoder.decode(value, { stream: true });
		let end: number;
		while ((end = buffer.indexOf('\n\n')) >= 0) {
			const event = buffer.slice(0, end);
			buffer = buffer.slice(end + 2);
			const data = event
				.split('\n')
				.filter((line) => line.startsWith('data:'))
				.map((line) => line.slice(5).trimStart())
				.join('\n');
			if (data) onData(data);
		}
	}
}

function sleep(ms: number, signal: AbortSignal): Promise<void> {
	return new Promise((resolve) => {
		const timer = setTimeout(resolve, ms);
		signal.addEventListener('abort', () => {
			clearTimeout(timer);
			resolve();
		});
	});
}
