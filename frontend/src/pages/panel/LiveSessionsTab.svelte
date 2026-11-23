<!--
@component
"Recargas en curso" section of the management panel: how the installation power is shared among the sessions in
progress, and a card per session with its card alias, start, duration, energy, cost so far, current tariff band,
energy per band and its recent power.
-->
<script lang="ts">
	import type { PanelLive, PanelLiveSession, PanelOverview } from '$lib/contract.gen';
	import { m } from '$lib/paraglide/messages';
	import { bandBackgrounds, bandLabels, formatEnergy, formatMoney, formatPower } from './panel';

	interface Props {
		overview: PanelOverview;
		live: PanelLive | undefined;
	}

	let { overview, live }: Props = $props();

	const colors = ['#0b7d70', '#2a62b8', '#7a4fbf', '#b5581b', '#3f7d20', '#a33a6b'];
	const maxPoints = 30;

	const sessions = $derived(live?.sessions ?? []);
	const total = $derived(live?.total_power_w ?? 0);
	const scale = $derived(Math.max(overview.max_power_w, total, 1));

	// Recent power of each session (one point per live update), kept while the panel is open.
	let history = $state.raw(new Map<string, number[]>());
	$effect(() => {
		const next = new Map<string, number[]>();
		for (const s of sessions) {
			const key = sessionKey(s);
			next.set(key, [...(history.get(key) ?? []), s.power_w].slice(-maxPoints));
		}
		history = next;
	});

	function sessionKey(s: PanelLiveSession): string {
		return `${s.station_name}/${s.connector_id}/${s.transaction_id}`;
	}

	function sparkline(points: number[]): string {
		if (points.length < 2) return '';
		const top = Math.max(...points, 1);
		return points.map((p, i) => `${(i / (points.length - 1)) * 260},${38 - (p / top) * 34}`).join(' ');
	}

	function bandShares(s: PanelLiveSession): number[] {
		const sum = s.energy_bands_wh.reduce((a, b) => a + b, 0);
		return sum > 0 ? s.energy_bands_wh.map((wh) => (wh / sum) * 100) : [0, 0, 0];
	}
</script>

<section class="rounded-xl border border-outline-variant bg-surface-container-lowest p-4 space-y-2">
	<div class="flex flex-wrap items-baseline justify-between gap-2">
		<h2 class="text-title-md font-semibold">{m.panel_live_title()}</h2>
		<span class="text-xs text-on-surface-variant">{m.panel_live_mode({ mode: overview.load_balancing_mode })}</span>
	</div>
	<div class="flex h-7 overflow-hidden rounded-md bg-surface-container" aria-label={m.panel_live_title()}>
		{#each sessions as s, i (sessionKey(s))}
			<span
				class="flex items-center justify-center overflow-hidden whitespace-nowrap text-[11px] font-semibold text-white transition-[width] duration-700"
				style="width: {(s.power_w / scale) * 100}%; background: {colors[i % colors.length]}"
				title="{s.station_name} [{s.connector_id}] · {formatPower(s.power_w)}">{s.card_alias}</span
			>
		{/each}
	</div>
	<div class="flex justify-between text-xs text-on-surface-variant tabular-nums">
		<span>0 kW</span>
		<span>{m.panel_live_total({ power: formatPower(total) })}</span>
		<span>{formatPower(scale)}</span>
	</div>
</section>

{#if sessions.length === 0}
	<p class="rounded-xl border border-dashed border-outline-variant p-6 text-center text-on-surface-variant">{m.panel_live_empty()}</p>
{:else}
	<div class="grid gap-3 grid-cols-[repeat(auto-fill,minmax(17rem,1fr))]">
		{#each sessions as s, i (sessionKey(s))}
			{@const shares = bandShares(s)}
			{@const color = colors[i % colors.length]}
			<article class="relative rounded-xl border border-outline-variant bg-surface-container-lowest p-4 space-y-3">
				<span class="absolute left-0 top-4 bottom-4 w-1 rounded-r" style="background: {color}"></span>
				<div class="flex items-start justify-between gap-2">
					<div class="min-w-0">
						<p class="font-semibold break-words">{s.card_alias}</p>
						<p class="font-mono text-xs text-on-surface-variant break-all">{s.station_name} · {m.panel_live_toma({ id: s.connector_id })}</p>
					</div>
					<span class="shrink-0 rounded px-2 py-0.5 text-xs font-semibold text-black bg-status-charging">{m.panel_live_charging()}</span>
				</div>
				<dl class="grid grid-cols-2 gap-3">
					<div><dt class="text-xs text-on-surface-variant">{m.panel_live_power()}</dt><dd class="text-lg font-semibold tabular-nums">{formatPower(s.power_w)}</dd></div>
					<div><dt class="text-xs text-on-surface-variant">{m.panel_live_energy()}</dt><dd class="text-lg font-semibold tabular-nums">{formatEnergy(s.energy_kwh)}</dd></div>
					<div><dt class="text-xs text-on-surface-variant">{m.panel_live_duration()}</dt><dd class="text-lg font-semibold tabular-nums">{s.duration || '—'}</dd></div>
					<div><dt class="text-xs text-on-surface-variant">{m.panel_live_cost()}</dt><dd class="text-lg font-semibold tabular-nums">{formatMoney(s.cost)}</dd></div>
				</dl>
				{#if (history.get(sessionKey(s)) ?? []).length > 1}
					<svg viewBox="0 0 260 40" preserveAspectRatio="none" class="w-full h-10" aria-hidden="true">
						<polyline points={sparkline(history.get(sessionKey(s)) ?? [])} fill="none" stroke={color} stroke-width="2" vector-effect="non-scaling-stroke" />
					</svg>
				{/if}
				<div class="space-y-1">
					<div class="flex h-2 overflow-hidden rounded bg-surface-container">
						<i class="block {bandBackgrounds.off_peak}" style="width: {shares[0]}%"></i>
						<i class="block {bandBackgrounds.flat}" style="width: {shares[1]}%"></i>
						<i class="block {bandBackgrounds.peak}" style="width: {shares[2]}%"></i>
					</div>
					<p class="text-xs text-on-surface-variant">
						{m.panel_live_started({ time: s.start_time })} · {m.panel_live_band_now({ band: bandLabels[s.current_band]() })}
					</p>
				</div>
			</article>
		{/each}
	</div>
{/if}
