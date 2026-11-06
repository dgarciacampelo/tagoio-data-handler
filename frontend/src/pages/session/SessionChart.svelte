<!--
@component
Power and energy chart of a session, the one of its email drawn as SVG: the mean power of each step as a green area
(kW, left scale) and the delivered energy as a blue line (kWh, right scale). Hovering a step shows its values.
-->
<script lang="ts">
	import type { SessionSeries } from '$lib/contract.gen';
	import { formatFixed } from '$lib/format';
	import { m } from '$lib/paraglide/messages';
	import {
		HEIGHT,
		MARGIN,
		PLOT_H,
		PLOT_W,
		WIDTH,
		chartGeometry,
		formatHourMinute,
		stepAt,
		stepBounds,
		stepValues
	} from './chart';

	let { series }: { series: SessionSeries } = $props();

	const geometry = $derived(chartGeometry(series));
	let hovered = $state<number | null>(null);
	const hover = $derived(hovered === null ? null : { ...stepValues(series, hovered), ...stepBounds(series, hovered) });

	function onPointerMove(event: PointerEvent) {
		const svg = event.currentTarget as SVGSVGElement;
		const box = svg.getBoundingClientRect();
		if (box.width === 0) return;
		hovered = stepAt(series, ((event.clientX - box.left) * WIDTH) / box.width);
	}
</script>

<figure class="space-y-2 max-w-[960px] mx-auto">
	<figcaption class="flex flex-wrap gap-x-6 gap-y-1 text-label-sm text-on-surface-variant">
		<span class="flex items-center gap-2"><span class="inline-block w-4 h-3 rounded-sm bg-[#16a34a]/40 border border-[#16a34a]"></span>{m.session_chart_power()}</span>
		<span class="flex items-center gap-2"><span class="inline-block w-4 h-0.5 bg-[#1d4ed8]"></span>{m.session_chart_energy()}</span>
	</figcaption>
	<div class="relative">
		<svg
			viewBox="0 0 {WIDTH} {HEIGHT}"
			class="w-full h-auto select-none touch-none"
			role="img"
			aria-label={m.session_chart_heading()}
			onpointermove={onPointerMove}
			onpointerleave={() => (hovered = null)}
			data-chart
		>
			<rect x="0" y="0" width={WIDTH} height={HEIGHT} fill="#ffffff" />
			{#each geometry.gridlines as line, g (g)}
				<line x1={MARGIN.left} x2={MARGIN.left + PLOT_W} y1={line.y} y2={line.y} stroke="#e5e5e5" />
				<text x={MARGIN.left - 6} y={line.y + 4} text-anchor="end" font-size="11" fill="#16a34a">{line.power}</text>
				<text x={MARGIN.left + PLOT_W + 6} y={line.y + 4} font-size="11" fill="#1d4ed8">{line.energy}</text>
			{/each}
			{#each geometry.ticks as tick (tick.x)}
				<line x1={tick.x} x2={tick.x} y1={MARGIN.top} y2={MARGIN.top + PLOT_H} stroke="#e5e5e5" />
				<text x={tick.x} y={MARGIN.top + PLOT_H + 16} text-anchor="middle" font-size="11" fill="#6b6b6b">{tick.label}</text>
			{/each}
			<line x1={MARGIN.left} x2={MARGIN.left + PLOT_W} y1={MARGIN.top + PLOT_H} y2={MARGIN.top + PLOT_H} stroke="#b0b0b0" />

			{#if hover}
				<rect x={hover.x} y={MARGIN.top} width={hover.width} height={PLOT_H} fill="#191c1e" fill-opacity="0.06" />
			{/if}
			{#each geometry.powerAreas as d, i (i)}
				<path {d} fill="#16a34a" fill-opacity="0.25" data-power-area />
			{/each}
			{#each geometry.powerLines as d, i (i)}
				<path {d} fill="none" stroke="#16a34a" stroke-width="1.5" />
			{/each}
			{#each geometry.energyLines as d, i (i)}
				<path {d} fill="none" stroke="#1d4ed8" stroke-width="2.5" stroke-linejoin="round" data-energy-line />
			{/each}
		</svg>

		{#if hover}
			<div
				class="absolute top-2 pointer-events-none bg-surface-container-lowest border border-outline-variant rounded-lg shadow-sm px-3 py-2 text-label-sm whitespace-nowrap"
				style="left: {Math.min((hover.x / WIDTH) * 100, 70)}%"
				data-tooltip
			>
				<div class="font-bold text-on-surface">{formatHourMinute(hover.from)} – {formatHourMinute(hover.to)}</div>
				{#if hover.powerKw === null}
					<div class="text-on-surface-variant">{m.session_chart_gap()}</div>
				{:else}
					<div class="text-[#15803d]">{formatFixed(hover.powerKw, 2)} kW</div>
					<div class="text-[#1d4ed8]">{formatFixed(hover.energyKwh ?? 0, 3)} kWh</div>
				{/if}
			</div>
		{/if}
	</div>
</figure>
