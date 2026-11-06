// Logic of the page of a finished session: its address, the stop motives, and how durations, power and times show.

import type { CostCheck, PaymentRecord, SessionDetail, TariffBand } from '$lib/contract.gen';
import { formatFixed } from '$lib/format';
import { m } from '$lib/paraglide/messages';
import { formatCents } from '../payments/payments';
import { TIME_ZONE } from './chart';

/** The page of a session (internal/sessionview.URL). */
export function sessionUrl(poolCode: number, stationName: string, connectorId: number, transactionId: number): string {
	return `/emsp-dashboard/sessions/${poolCode}/${encodeURIComponent(stationName)}/${connectorId}/${transactionId}`;
}

/** The page that finds a session by its transaction id alone. */
export function transactionUrl(transactionId: number): string {
	return `/emsp-dashboard/sessions/${transactionId}`;
}

const stopMotives: Record<string, () => string> = {
	CAR: m.session_stop_car,
	COST: m.session_stop_cost,
	CSMS: m.session_stop_csms,
	DISCONNECT: m.session_stop_disconnect,
	EVSE: m.session_stop_evse,
	RESERVED: m.session_stop_reserved,
	USER: m.session_stop_user,
	OTHER: m.session_stop_other
};

/** Why the session stopped, as the CSMS sent it (StopMotive of neos-ocpp). */
export function stopMotiveLabel(motive: string): string {
	if (!motive) return m.session_stop_unknown();
	return stopMotives[motive.toUpperCase()]?.() ?? motive;
}

export const bandLabels: Record<TariffBand, () => string> = {
	off_peak: m.session_band_off_peak,
	flat: m.session_band_flat,
	peak: m.session_band_peak
};

export const bandColors: Record<TariffBand, string> = {
	off_peak: 'bg-band-off-peak text-on-band-off-peak',
	flat: 'bg-band-flat text-on-band-flat',
	peak: 'bg-band-peak text-on-band-peak'
};

export const costCheckLabels: Record<CostCheck, () => string> = {
	ok: m.session_cost_ok,
	capped: m.session_cost_capped,
	mismatch: m.session_cost_mismatch
};

/** How a settled payment compares with the session's cost: in full, short of it, or over it. */
export type ChargeCheck = 'ok' | 'short' | 'over';

export interface ChargeComparison {
	check: ChargeCheck;
	charged: number; // Cents the card was charged
	cost: number; // Cents the session cost, as the CSMS stored it
	difference: number; // charged - cost
}

/**
 * What the card was charged against what the session cost, for a settled (COMMITTED) payment; null for any other.
 * The bands check only compares the CSMS's own figures: this one catches a payment settled before the session ended
 * (seen on PROD: 0,31 € charged of 2,37 €, settled by a reconnect while the car kept charging). A cent apart is rounding.
 */
export function chargeComparison(session: SessionDetail, payment: PaymentRecord): ChargeComparison | null {
	if (payment.status !== 'COMMITTED' || payment.amount === null) return null;
	const cost = Math.round(session.cost * 100);
	const difference = payment.amount - cost;
	const check: ChargeCheck = Math.abs(difference) <= 1 ? 'ok' : difference < 0 ? 'short' : 'over';
	return { check, charged: payment.amount, cost, difference };
}

/** The sentence of a charge comparison, "Se cobraron 0,31 € de 2,37 €: 2,06 € sin cobrar". */
export function chargeLabel(comparison: ChargeComparison): string {
	if (comparison.check === 'ok') return m.session_charge_ok();
	const amounts = {
		charged: formatCents(comparison.charged),
		cost: formatCents(comparison.cost),
		difference: formatCents(Math.abs(comparison.difference))
	};
	return comparison.check === 'short' ? m.session_charge_short(amounts) : m.session_charge_over(amounts);
}

/** A duration, "1 h 40 min", "12 min" or "35 s"; "—" when unknown. */
export function formatDuration(seconds: number | null): string {
	if (seconds === null) return '—';
	if (seconds < 60) return `${seconds} s`;
	const minutes = Math.round(seconds / 60);
	const hours = Math.floor(minutes / 60);
	return hours === 0 ? `${minutes} min` : `${hours} h ${minutes % 60} min`;
}

/** Watts as kW with one decimal, "7,2 kW"; "—" when unknown. */
export function formatPower(watts: number | null): string {
	return watts === null ? '—' : `${formatFixed(watts / 1000, 1)} kW`;
}

/** Wh as kWh with three decimals, as the audits show it, "6,000 kWh". */
export function formatKwh(wh: number): string {
	return `${formatFixed(wh / 1000, 3)} kWh`;
}

/** The price per kWh the session came to, "0,4250 €/kWh"; "—" without energy. */
export function formatMeanPrice(session: SessionDetail): string {
	return session.energy_wh > 0 ? `${formatFixed(session.cost / (session.energy_wh / 1000), 4)} €/kWh` : '—';
}

/** The share of a band in the session's energy, "83 %". */
export function bandShare(session: SessionDetail, energyWh: number): string {
	const total = session.bands.reduce((sum, band) => sum + band.energy_wh, 0);
	return total > 0 ? `${Math.round((energyWh / total) * 100)} %` : '—';
}

const dayTime = new Intl.DateTimeFormat('es-ES', {
	timeZone: TIME_ZONE,
	day: '2-digit',
	month: '2-digit',
	year: 'numeric',
	hour: '2-digit',
	minute: '2-digit',
	hourCycle: 'h23'
});

// Its parts are put together by hand: ICU's Spanish pattern for a day and month without year drops a zero ("4/11")
const shortDayTime = new Intl.DateTimeFormat('en-GB', {
	timeZone: TIME_ZONE,
	day: '2-digit',
	month: '2-digit',
	hour: '2-digit',
	minute: '2-digit',
	second: '2-digit',
	hourCycle: 'h23'
});

/** An ISO instant in the pools' time zone, "04/11/2026, 19:00"; "—" when unknown. */
export function formatInstant(iso: string | null): string {
	if (!iso) return '—';
	const instant = Date.parse(iso);
	return Number.isNaN(instant) ? iso : dayTime.format(instant);
}

/** The time of a meter value, "04/11, 19:01:00" (a session can span midnight); the raw text when unreadable. */
export function formatMeterTime(iso: string): string {
	const instant = Date.parse(iso);
	if (Number.isNaN(instant)) return iso;
	const p = Object.fromEntries(shortDayTime.formatToParts(instant).map((part) => [part.type, part.value]));
	return `${p.day}/${p.month}, ${p.hour}:${p.minute}:${p.second}`;
}
