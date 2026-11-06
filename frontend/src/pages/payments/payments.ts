// Logic of the payments page: the default range, the status a payment is shown with, and the amounts.

import type { PaymentRecord } from '$lib/contract.gen';
import { formatFixed } from '$lib/format';
import { m } from '$lib/paraglide/messages';

/** A PENDING payment older than this is a checkout the driver never finished (or a request that failed silently). */
export const ABANDONED_AFTER_MS = 15 * 60 * 1000;

export const DEFAULT_RANGE_DAYS = 7;

/** The status a payment is shown with: the stored one, or ABANDONED for an old PENDING payment. */
export type DisplayStatus = 'PENDING' | 'AUTHORIZED' | 'COMMITTED' | 'CANCELED' | 'ERROR' | 'ABANDONED';

export const DISPLAY_STATUSES: DisplayStatus[] = ['COMMITTED', 'CANCELED', 'ERROR', 'ABANDONED', 'AUTHORIZED', 'PENDING'];

export const STATUS_LABELS: Record<DisplayStatus, () => string> = {
	COMMITTED: m.payments_status_committed,
	CANCELED: m.payments_status_canceled,
	ERROR: m.payments_status_error,
	ABANDONED: m.payments_status_abandoned,
	AUTHORIZED: m.payments_status_authorized,
	PENDING: m.payments_status_pending
};

export const STATUS_CLASSES: Record<DisplayStatus, string> = {
	COMMITTED: 'bg-status-available text-black',
	CANCELED: 'bg-surface-container-high text-on-surface-variant',
	ERROR: 'bg-error-container text-error',
	ABANDONED: 'bg-status-unavailable text-black',
	AUTHORIZED: 'bg-status-charging text-black',
	PENDING: 'bg-status-preparing text-black'
};

/** Adds days to a "2006-01-02" date. */
export function addDays(date: string, days: number): string {
	const [year, month, day] = date.split('-').map(Number);
	return new Date(Date.UTC(year, month - 1, day + days)).toISOString().slice(0, 10);
}

/** The last DEFAULT_RANGE_DAYS days, today included. */
export function defaultRange(today: string): { from: string; to: string } {
	return { from: addDays(today, 1 - DEFAULT_RANGE_DAYS), to: today };
}

export function displayStatus(payment: PaymentRecord, now: number = Date.now()): DisplayStatus {
	const status = payment.status as DisplayStatus;
	if (status === 'PENDING' && now - Date.parse(payment.created_at) > ABANDONED_AFTER_MS) {
		return 'ABANDONED';
	}
	return DISPLAY_STATUSES.includes(status) ? status : 'PENDING';
}

/** Cents as euros, "12,34 €". */
export function formatCents(cents: number | null): string {
	return cents === null ? '—' : `${formatFixed(cents / 100, 2)} €`;
}

const dateTime = new Intl.DateTimeFormat('es-ES', {
	timeZone: 'Europe/Madrid',
	day: '2-digit',
	month: '2-digit',
	year: 'numeric',
	hour: '2-digit',
	minute: '2-digit'
});

/** An ISO instant as Madrid local time, "05/11/2026, 11:02". */
export function formatDateTime(iso: string | null): string {
	if (!iso) return '—';
	const instant = Date.parse(iso);
	return Number.isNaN(instant) ? iso : dateTime.format(instant);
}

/** Wh as kWh, "9,87 kWh". */
export function formatEnergy(wh: number | null): string {
	return wh === null ? '—' : `${formatFixed(wh / 1000, 2)} kWh`;
}

/** The error of a payment, "PREAUTH 562: Paycomet refused the preauth", or '' when it has none. */
export function errorText(payment: PaymentRecord): string {
	if (!payment.error_stage && !payment.error_message) return '';
	const where = [payment.error_stage, payment.error_code].filter((x) => x !== null && x !== '').join(' ');
	return payment.error_message ? `${where}: ${payment.error_message}` : where;
}

export interface PaymentsSummary {
	counts: Record<DisplayStatus, number>;
	committedCents: number; // Captured on LIVE payments: TEST ones move no money
}

export function summarize(payments: PaymentRecord[], now: number = Date.now()): PaymentsSummary {
	const counts = Object.fromEntries(DISPLAY_STATUSES.map((s) => [s, 0])) as Record<DisplayStatus, number>;
	let committedCents = 0;
	for (const payment of payments) {
		const status = displayStatus(payment, now);
		counts[status]++;
		if (status === 'COMMITTED' && payment.category === 'LIVE') {
			committedCents += payment.amount ?? 0;
		}
	}
	return { counts, committedCents };
}
