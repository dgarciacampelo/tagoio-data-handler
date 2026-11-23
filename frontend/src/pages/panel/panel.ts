// Presentation helpers of the management panel: number and date formats, tariff bands and connection states.

import { formatFixed } from '$lib/format';
import { m } from '$lib/paraglide/messages';
import type { PanelConnection, PanelTariffPeriod, TariffBand } from '$lib/contract.gen';

/** The panel's sections, as the TagoIO tabs open them (#general, #tarifas, #cpo, #en-curso). */
export type Section = 'general' | 'tarifas' | 'cpo' | 'en-curso';

export const sections: { id: Section; label: () => string }[] = [
	{ id: 'general', label: m.panel_tab_general },
	{ id: 'tarifas', label: m.panel_tab_tariffs },
	{ id: 'cpo', label: m.panel_tab_cpo },
	{ id: 'en-curso', label: m.panel_tab_live }
];

/** The section of a URL hash; undefined when it names none (the panel then shows its own tabs). */
export function sectionOf(hash: string): Section | undefined {
	const id = hash.replace(/^#/, '');
	return sections.find((s) => s.id === id)?.id;
}

export function formatPower(watts: number): string {
	return `${formatFixed(watts / 1000, 1)} kW`;
}

export function formatEnergy(kwh: number, decimals = 2): string {
	return `${formatFixed(kwh, decimals)} kWh`;
}

export function formatMoney(euros: number): string {
	return `${formatFixed(euros, 2)} €`;
}

/** "2 h 05 min", or "45 min". */
export function formatMinutes(minutes: number): string {
	const h = Math.floor(minutes / 60);
	const min = Math.round(minutes % 60);
	return h > 0 ? `${h} h ${String(min).padStart(2, '0')} min` : `${min} min`;
}

/** "2026-11-20T12:17" (server local time) as "20/11/2026 12:17". */
export function formatLocalDateTime(value: string): string {
	const match = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}:\d{2})/.exec(value);
	return match ? `${match[3]}/${match[2]}/${match[1]} ${match[4]}` : value;
}

/** How long ago an RFC 3339 instant was, in Spanish ("hace 5 min"); "" when unknown. */
export function sinceText(iso: string | null | undefined, now: number): string {
	if (!iso) return '';
	const seconds = Math.max(0, Math.round((now - Date.parse(iso)) / 1000));
	if (Number.isNaN(seconds)) return '';
	if (seconds < 60) return 'hace un momento';
	const minutes = Math.round(seconds / 60);
	if (minutes < 60) return `hace ${minutes} min`;
	const hours = Math.round(minutes / 60);
	if (hours < 48) return `hace ${hours} h`;
	return `hace ${Math.round(hours / 24)} días`;
}

/** A date as YYYY-MM-DD (local), for the history filter. */
export function isoDate(date: Date): string {
	const pad = (n: number) => String(n).padStart(2, '0');
	return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`;
}

export const bandLabels: Record<TariffBand, () => string> = {
	off_peak: m.panel_band_off_peak,
	flat: m.panel_band_flat,
	peak: m.panel_band_peak
};

/** Tailwind fill class of each band (theme tokens). */
export const bandFills: Record<TariffBand, string> = {
	off_peak: 'fill-band-off-peak-strong',
	flat: 'fill-band-flat-strong',
	peak: 'fill-band-peak-strong'
};

export const bandBackgrounds: Record<TariffBand, string> = {
	off_peak: 'bg-band-off-peak-strong',
	flat: 'bg-band-flat-strong',
	peak: 'bg-band-peak-strong'
};

/** The band of a weekday hour, from the overview's periods. */
export function bandAt(periods: PanelTariffPeriod[], hour: number): TariffBand {
	return periods.find((p) => hour >= p.from_hour && hour < p.to_hour)?.band ?? 'off_peak';
}

export const connectionLabels: Record<PanelConnection, () => string> = {
	Online: m.panel_connection_online,
	Booting: m.panel_connection_booting,
	Offline: m.panel_connection_offline
};

export const connectionPills: Record<PanelConnection, string> = {
	Online: 'bg-primary-fixed text-on-primary-fixed',
	Booting: 'bg-band-flat text-on-band-flat',
	Offline: 'bg-error-container text-error'
};

/** The description of each load balancing mode, by its label. */
export function modeDescription(label: string): string {
	switch (label) {
		case 'Ninguno':
			return m.panel_mode_none();
		case 'Estático normal':
			return m.panel_mode_static();
		case 'Dinámico normal':
			return m.panel_mode_dynamic();
		case 'Dinámico boost':
			return m.panel_mode_boost();
		default:
			return '';
	}
}
