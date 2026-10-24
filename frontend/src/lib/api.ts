// Minimal JSON client of the server APIs. The contract types come from Go (contract.gen.ts).

import type { APIError, ErrorCode } from './contract.gen';

/** A failed API call. `code` is the server error code (translate it with errorMessage()). */
export class ApiError extends Error {
	constructor(
		readonly status: number,
		readonly code: ErrorCode | undefined,
		message: string
	) {
		super(message);
		this.name = 'ApiError';
	}
}

/**
 * Sends a JSON request (same origin, cookies included). Resolves with the parsed JSON answer (undefined for 202 or
 * 204), or throws ApiError. An admin API answering 401 sends the browser to the login page, which comes back.
 */
export async function apiFetch<T = void>(
	method: 'GET' | 'POST' | 'PUT' | 'DELETE',
	url: string,
	body?: unknown,
	signal?: AbortSignal
): Promise<T> {
	let response: Response;
	try {
		response = await fetch(url, {
			method,
			headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
			body: body === undefined ? undefined : JSON.stringify(body),
			credentials: 'same-origin',
			signal
		});
	} catch (error) {
		if (signal?.aborted) throw error; // Let callers ignore cancelled requests
		throw new ApiError(0, undefined, `Network error: ${String(error)}`);
	}

	if (!response.ok) {
		const error = (await response.json().catch(() => undefined)) as APIError | undefined;
		if (response.status === 401 && error?.code === 'unauthenticated' && url.startsWith('/api/admin/')) {
			goToLogin();
			return new Promise<never>(() => {}); // The page is being replaced: never settle
		}
		throw new ApiError(response.status, error?.code, error?.message ?? response.statusText);
	}
	if (response.status === 202 || response.status === 204) {
		return undefined as T;
	}
	return (await response.json()) as T;
}

/** Sends the browser to the admin login page, which comes back to the current page. */
export function goToLogin(): void {
	const back = window.location.pathname + window.location.search;
	window.location.assign(`/login?redirect=${encodeURIComponent(back)}`);
}
