<!--
@component
"General" section of the management panel: a summary of the installation, the load balancing mode (changed in
place, without a dialog), the state of each station with its RESET button, and the history of finished sessions.
-->
<script lang="ts">
	import { errorMessage } from '$lib/errors';
	import type { PanelLive, PanelOverview, PanelResetResult } from '$lib/contract.gen';
	import Icon from '$lib/components/Icon.svelte';
	import { statusStyle } from '$lib/status';
	import { m } from '$lib/paraglide/messages';
	import type { PanelClient } from './client';
	import { connectionLabels, connectionPills, formatPower, modeDescription, sinceText } from './panel';
	import HistoryTable from './HistoryTable.svelte';

	interface Props {
		client: PanelClient;
		overview: PanelOverview;
		live: PanelLive | undefined;
		onoverview: (overview: PanelOverview) => void;
		notify: (message: string) => void;
	}

	let { client, overview, live, onoverview, notify }: Props = $props();

	let pendingMode = $state('');
	let confirming = $state('');
	let resetting = $state<Record<string, boolean>>({});
	let now = $state(Date.now());

	$effect(() => {
		const timer = setInterval(() => (now = Date.now()), 30000);
		return () => clearInterval(timer);
	});

	const stations = $derived(live?.stations ?? []);
	const online = $derived(stations.filter((s) => s.connection === 'Online').length);
	const sessions = $derived(live?.sessions.length ?? 0);
	const totalPower = $derived(live?.total_power_w ?? 0);
	const powerShare = $derived(overview.max_power_w > 0 ? Math.min(100, (totalPower / overview.max_power_w) * 100) : 0);

	async function changeMode(mode: string) {
		if (mode === overview.load_balancing_mode || pendingMode) return;
		pendingMode = mode;
		try {
			onoverview(await client.call<PanelOverview>('PUT', '/load-balancing-mode', { mode }));
			notify(m.panel_mode_changed({ mode }));
		} catch (error) {
			notify(errorMessage(error));
		} finally {
			pendingMode = '';
		}
	}

	async function reset(station: string) {
		confirming = '';
		resetting = { ...resetting, [station]: true };
		try {
			await client.call<PanelResetResult>('POST', `/stations/${encodeURIComponent(station)}/reset`);
			notify(m.panel_reset_sent({ station }));
		} catch (error) {
			notify(errorMessage(error));
		} finally {
			resetting = { ...resetting, [station]: false };
		}
	}
</script>

<section class="grid gap-3 grid-cols-[repeat(auto-fit,minmax(12rem,1fr))]" aria-label={m.panel_tab_general()}>
	<div class="rounded-xl border border-outline-variant bg-surface-container-lowest p-4">
		<p class="text-xs text-on-surface-variant">{m.panel_kpi_online()}</p>
		<p class="text-2xl font-semibold tabular-nums">{m.panel_kpi_online_value({ online, total: stations.length })}</p>
	</div>
	<div class="rounded-xl border border-outline-variant bg-surface-container-lowest p-4">
		<p class="text-xs text-on-surface-variant">{m.panel_kpi_sessions()}</p>
		<p class="text-2xl font-semibold tabular-nums">{sessions}</p>
	</div>
	<div class="rounded-xl border border-outline-variant bg-surface-container-lowest p-4 space-y-2">
		<p class="text-xs text-on-surface-variant">{m.panel_kpi_power()}</p>
		<p class="text-2xl font-semibold tabular-nums">
			{formatPower(totalPower)}
			<span class="text-sm font-normal text-on-surface-variant">{m.panel_kpi_power_of({ max: formatPower(overview.max_power_w) })}</span>
		</p>
		<div class="h-1.5 rounded-full bg-surface-container overflow-hidden">
			<div class="h-full bg-primary transition-[width] duration-700" style="width: {powerShare}%"></div>
		</div>
	</div>
</section>

<section class="rounded-xl border border-outline-variant bg-surface-container-lowest p-4 space-y-3">
	<div class="flex flex-wrap items-baseline justify-between gap-2">
		<h2 class="text-title-md font-semibold">{m.panel_mode_title()}</h2>
		<span class="text-xs text-on-surface-variant">{m.panel_mode_hint()}</span>
	</div>
	<div class="grid gap-2 grid-cols-[repeat(auto-fit,minmax(11rem,1fr))]" role="radiogroup" aria-label={m.panel_mode_title()}>
		{#each overview.balancing_modes as mode (mode)}
			{@const selected = mode === overview.load_balancing_mode}
			<button
				type="button"
				role="radio"
				aria-checked={selected}
				disabled={!!pendingMode}
				class="text-left rounded-lg border p-3 transition-colors {selected
					? 'border-primary bg-primary-fixed/40 ring-1 ring-primary'
					: 'border-outline-variant hover:border-primary'}"
				onclick={() => changeMode(mode)}
			>
				<span class="block font-semibold">
					{mode}
					{#if pendingMode === mode}<span class="font-normal text-on-surface-variant"> · {m.panel_mode_applying()}</span>{/if}
				</span>
				<span class="block text-xs text-on-surface-variant">{modeDescription(mode)}</span>
			</button>
		{/each}
	</div>
</section>

<section class="rounded-xl border border-outline-variant bg-surface-container-lowest p-4 space-y-3">
	<h2 class="text-title-md font-semibold">{m.panel_stations_title()}</h2>
	{#if stations.length === 0}
		<p class="text-on-surface-variant">{m.panel_stations_empty()}</p>
	{:else}
		<div class="grid gap-3 grid-cols-[repeat(auto-fill,minmax(15rem,1fr))]">
			{#each stations as station (station.name)}
				<article
					class="rounded-lg border border-outline-variant p-3 space-y-3 {station.connection === 'Offline'
						? 'bg-error-container/30'
						: ''}"
				>
					<div class="flex items-center justify-between gap-2">
						<span class="font-mono text-sm break-all">{station.name}</span>
						<span class="shrink-0 rounded-full px-2 py-0.5 text-xs font-semibold {connectionPills[station.connection]}">
							{connectionLabels[station.connection]()}
						</span>
					</div>
					<ul class="space-y-1.5">
						{#each station.connectors as connector (connector.id)}
							{@const style = statusStyle(connector.status)}
							<li class="flex items-center gap-2 text-sm">
								<span class="w-14 text-on-surface-variant">{m.panel_connector({ id: connector.id })}</span>
								<span class="rounded px-2 py-0.5 text-xs font-semibold text-black {style.background}">{style.label()}</span>
								<span class="ml-auto text-xs text-on-surface-variant">{sinceText(connector.since, now)}</span>
							</li>
						{/each}
					</ul>
					<div class="flex flex-wrap items-center justify-between gap-2 border-t border-dashed border-outline-variant pt-2 text-xs text-on-surface-variant">
						{#if confirming === station.name}
							<span>{m.panel_reset_question()}</span>
							<span class="flex gap-2">
								<button type="button" class="rounded border border-outline-variant px-2 py-1" onclick={() => (confirming = '')}>
									{m.panel_reset_no()}
								</button>
								<button type="button" class="rounded bg-error text-white px-2 py-1 font-semibold" onclick={() => reset(station.name)}>
									{m.panel_reset_yes()}
								</button>
							</span>
						{:else}
							<span>{station.connection_since ? m.panel_since({ when: sinceText(station.connection_since, now) }) : ''}</span>
							<button
								type="button"
								class="inline-flex items-center gap-1 rounded border border-error/50 px-2 py-1 font-semibold text-error disabled:opacity-50"
								disabled={station.connection !== 'Online' || resetting[station.name]}
								title={station.connection !== 'Online' ? m.panel_reset_offline() : undefined}
								onclick={() => (confirming = station.name)}
							>
								<Icon name="sync" size={16} class={resetting[station.name] ? 'animate-spin' : ''} />
								{m.panel_reset()}
							</button>
						{/if}
					</div>
				</article>
			{/each}
		</div>
	{/if}
</section>

<HistoryTable {client} {notify} />
