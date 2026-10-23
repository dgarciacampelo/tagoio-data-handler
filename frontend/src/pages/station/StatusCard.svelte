<!--
@component
Status of the selected connector, in the status color. During a session it comes with a second card: the stop
button of a paid session, the RFID hint of a private one, or a spinner until the session data arrives.
-->
<script lang="ts">
	import { apiFetch } from '$lib/api';
	import type { StationLive, StopRequest } from '$lib/contract.gen';
	import Icon from '$lib/components/Icon.svelte';
	import { m } from '$lib/paraglide/messages';
	import { statusStyle } from '$lib/status';

	interface Props {
		live: StationLive;
		poolCode: number;
		stationName: string;
		connectorId: number;
	}

	let { live, poolCode, stationName, connectorId }: Props = $props();

	const style = $derived(statusStyle(live.status));
	const inSession = $derived(live.session_card !== 'none');

	let stop = $state<'idle' | 'sending' | 'sent' | 'failed'>('idle');

	async function requestStop() {
		stop = 'sending';
		try {
			const body: StopRequest = { pool_code: poolCode, station_name: stationName, connector_id: connectorId };
			await apiFetch('POST', '/api/stop-request', body);
			stop = 'sent';
		} catch {
			stop = 'failed';
		}
	}
</script>

{#snippet statusHeading(pulse: boolean)}
	<div class="flex flex-col gap-xs">
		<div class="inline-flex items-center gap-xs font-label-caps text-label-caps text-black">
			<span class="w-2 h-2 rounded-full bg-black {pulse ? 'animate-pulse' : ''}"></span>
			{style.label()}
		</div>
		<h2 class="font-headline-md text-headline-md text-on-surface">{poolCode}/{stationName}</h2>
		<p class="font-body-md text-on-surface opacity-80">{m.station_connector({ id: connectorId })}</p>
	</div>
{/snippet}

{#if !live.config_loaded}
	<section
		class="border border-outline-variant rounded-xl shadow-sm bg-surface-container-lowest p-xl min-h-[12rem] flex-1 w-full flex flex-col items-center justify-center gap-md"
	>
		<Icon name="progressActivity" size={48} class="text-primary animate-spin" />
		<h3 class="font-headline-md text-on-surface">{m.station_loading_title()}</h3>
		<p class="font-label-sm text-on-surface-variant">{m.station_loading_text()}</p>
	</section>
{:else if inSession}
	<div class="grid grid-cols-1 sm:grid-cols-5 gap-grid-gutter w-full flex-1">
		<section
			class="sm:col-span-3 border border-outline-variant rounded-xl shadow-sm border-t-[4px] p-xl flex flex-col justify-center gap-md transition-colors h-full {style.background}"
		>
			{@render statusHeading(live.status === 'Charging')}
		</section>

		<div
			class="sm:col-span-2 border border-outline-variant rounded-xl shadow-sm bg-surface-container-lowest p-lg flex flex-col items-center justify-center gap-md h-full text-center"
		>
			{#if live.session_card === 'stop'}
				<div class="flex flex-col gap-base">
					<h3 class="font-headline-md text-on-surface">{m.station_stop_title()}</h3>
					<p class="font-label-sm text-on-surface-variant">{m.station_stop_text()}</p>
				</div>
				<button
					type="button"
					onclick={requestStop}
					disabled={stop === 'sending' || stop === 'sent'}
					class="w-full mt-sm h-12 bg-error hover:opacity-90 text-on-primary font-label-caps text-label-caps px-md rounded-lg shadow-sm transition-opacity flex items-center justify-center gap-xs disabled:opacity-50 disabled:cursor-not-allowed"
				>
					<Icon name={stop === 'sending' ? 'hourglassEmpty' : 'powerOff'} size={20} class={stop === 'sending' ? 'animate-spin' : ''} />
					{stop === 'sent' ? m.station_stop_sent() : m.station_stop_button()}
				</button>
				{#if stop === 'failed'}
					<p class="text-error text-label-sm">{m.station_stop_error()}</p>
				{/if}
			{:else if live.session_card === 'private'}
				<Icon name="lockPerson" size={32} class="text-primary" />
				<div class="flex flex-col gap-base mt-sm">
					<h3 class="font-headline-md text-on-surface">{m.station_private_title()}</h3>
					<p class="font-label-sm text-on-surface-variant">
						{m.station_private_text()}<br />
						<strong>{m.station_private_hint_strong()}</strong>
						{m.station_private_hint_rest()}
					</p>
				</div>
			{:else}
				<Icon name="sync" size={32} class="text-primary animate-spin" />
				<div class="flex flex-col gap-base mt-sm">
					<h3 class="font-headline-md text-on-surface">{m.station_starting_title()}</h3>
					<p class="font-label-sm text-on-surface-variant">{m.station_starting_text()}</p>
				</div>
			{/if}
		</div>
	</div>
{:else}
	<section
		class="border border-outline-variant rounded-xl shadow-sm border-t-[4px] p-xl min-h-[12rem] flex-1 w-full flex flex-col sm:flex-row justify-between items-start sm:items-center gap-md transition-colors {style.background}"
	>
		{@render statusHeading(false)}
	</section>
{/if}
