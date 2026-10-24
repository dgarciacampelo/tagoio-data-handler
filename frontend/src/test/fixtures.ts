// View models of the page tests, shaped like the Go server's (internal/handlers, internal/dto).

import type { AuditSession, StationLive, StationPage } from '$lib/contract.gen';

/** The live part of a station page: an available connector of a loaded pool, unless overridden. */
export function stationLive(overrides: Partial<StationLive> = {}): StationLive {
	return {
		config_loaded: true,
		status: 'Available',
		panel: 'plug_in',
		session_card: 'none',
		show_metrics: false,
		metrics: null,
		operator: {
			name: 'Velo Energy',
			fiscal_id: 'B00000000',
			address: 'Calle Mayor 1, Madrid',
			phone: '900 000 000',
			phone_href: 'tel:+34900000000',
			email: 'info@example.com',
			email_href: 'mailto:info@example.com',
			web: 'https://example.com'
		},
		rates: { off_peak: 0.25, flat: 0.35, peak: 0.45, vat_percent: 21, preauth_amount: 50 },
		...overrides
	};
}

/** The station page of connector 1 of station EVC01 (pool 12). */
export function stationPage(live: StationLive = stationLive()): StationPage {
	return {
		pool_code: 12,
		station_name: 'EVC01',
		connector_id: 1,
		connectors: [],
		poll_url: '/api/station/12/EVC01?cid=1',
		live
	};
}

/** A finished session of the audits page. */
export function auditSession(overrides: Partial<AuditSession> = {}): AuditSession {
	const transactionId = overrides.transaction_id ?? 7783;
	return {
		transaction_id: transactionId,
		pool_code: 12,
		station_name: 'EVC01',
		connector_id: 1,
		card_alias: 'Pago TPV',
		start_date: '2026-10-20 18:30',
		time_band: '2.0TD',
		total_energy_kwh: 12.345,
		cost: 4.32,
		export_url: `/api/admin/audits/${transactionId}/export`,
		off_peak_wh: 0,
		flat_wh: 2345,
		peak_wh: 10000,
		rate_off_peak: 0.25,
		rate_flat: 0.35,
		rate_peak: 0.45,
		...overrides
	};
}
