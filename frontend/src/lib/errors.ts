// Translation of the server error codes. The Record is exhaustive: a new code in Go (internal/dto) fails the type
// check until it has a message here.

import { m } from '$lib/paraglide/messages';
import { ApiError } from './api';
import type { ErrorCode } from './contract.gen';

const messages: Record<ErrorCode, () => string> = {
	invalid_request: m.error_invalid_request,
	invalid_credentials: m.error_invalid_credentials,
	unauthenticated: m.error_unauthenticated,
	not_found: m.error_not_found,
	conflict: m.error_conflict,
	rate_limited: m.error_rate_limited,
	service_unavailable: m.error_service_unavailable,
	internal_error: m.error_internal_error
};

/** Returns the message to show for a failed call. */
export function errorMessage(error: unknown): string {
	if (error instanceof ApiError && error.code) {
		return messages[error.code]();
	}
	return m.error_network();
}
