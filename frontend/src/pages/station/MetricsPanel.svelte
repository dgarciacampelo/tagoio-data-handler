<!--
@component
Live values of the charging session: energy, power, duration and cost ("--" until the session data arrives).
-->
<script lang="ts">
	import type { SessionMetrics } from '$lib/contract.gen';
	import Icon from '$lib/components/Icon.svelte';
	import type { IconName } from '$lib/icons';
	import { formatFixed } from '$lib/format';
	import { m } from '$lib/paraglide/messages';

	let { metrics }: { metrics: SessionMetrics | null } = $props();

	const cards = $derived<{ label: string; icon: IconName; value: string; unit: string }[]>([
		{ label: m.station_metric_energy(), icon: 'bolt', value: metrics ? formatFixed(metrics.energy_kwh, 2) : '--', unit: 'kWh' },
		{ label: m.station_metric_power(), icon: 'speed', value: metrics ? formatFixed(metrics.power_kw, 1) : '--', unit: 'kW' },
		{ label: m.station_metric_duration(), icon: 'timer', value: metrics?.duration_minutes || '--', unit: 'min' },
		{ label: m.station_metric_cost(), icon: 'euroSymbol', value: metrics ? formatFixed(metrics.cost, 2) : '--', unit: 'EUR' }
	]);
</script>

<section class="grid grid-cols-1 md:grid-cols-4 gap-grid-gutter">
	{#each cards as card (card.icon)}
		<div class="bg-surface-container-lowest border border-outline-variant rounded-xl shadow-sm p-md flex flex-col gap-xs">
			<div class="flex justify-between items-center text-on-surface-variant">
				<span class="font-label-sm text-label-sm uppercase tracking-wider">{card.label}</span>
				<Icon name={card.icon} size={20} />
			</div>
			<div class="flex items-baseline gap-xs">
				<span class="font-display-metrics text-display-metrics text-on-surface">{card.value}</span>
				<span class="font-label-sm text-label-sm text-on-surface-variant">{card.unit}</span>
			</div>
		</div>
	{/each}
</section>
