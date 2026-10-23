<!--
@component
"CPO / Tarifas" tab: the operator of the station and its rates, with the note about the payment hold.
-->
<script lang="ts">
	import type { OperatorInfo, RatesInfo } from '$lib/contract.gen';
	import Icon from '$lib/components/Icon.svelte';
	import { formatUpTo } from '$lib/format';
	import { m } from '$lib/paraglide/messages';

	let { operator, rates }: { operator: OperatorInfo; rates: RatesInfo } = $props();

	const row = 'flex justify-between items-center border-b border-outline-variant pb-2 gap-4';
</script>

<div class="grid grid-cols-1 lg:grid-cols-2 gap-md items-start">
	<div class="bg-surface-container-lowest border border-outline-variant rounded-xl p-lg">
		<h2 class="font-headline-md text-on-surface mb-md">{m.station_cpo_title()}</h2>
		<div class="space-y-sm">
			<div class={row}>
				<span class="text-on-surface-variant whitespace-nowrap">{m.station_cpo_company()}</span>
				<span class="font-bold text-right text-on-surface">{operator.name}</span>
			</div>
			<div class={row}>
				<span class="text-on-surface-variant whitespace-nowrap">{m.station_cpo_fiscal_id()}</span>
				<span class="font-bold text-right text-on-surface">{operator.fiscal_id}</span>
			</div>
			<div class={row}>
				<span class="text-on-surface-variant whitespace-nowrap">{m.station_cpo_address()}</span>
				<span class="font-bold text-right text-on-surface">{operator.address}</span>
			</div>
			<div class={row}>
				<span class="text-on-surface-variant whitespace-nowrap">{m.station_cpo_phone()}</span>
				<a href={operator.phone_href} class="font-bold text-right text-primary hover:underline">{operator.phone}</a>
			</div>
			<div class={row}>
				<span class="text-on-surface-variant whitespace-nowrap">{m.station_cpo_email()}</span>
				<a href={operator.email_href} class="font-bold text-right text-primary hover:underline break-all">{operator.email}</a>
			</div>
			<div class="flex justify-between items-center border-outline-variant pb-2 gap-4">
				<span class="text-on-surface-variant whitespace-nowrap">{m.station_cpo_web()}</span>
				<span class="font-bold text-right text-on-surface-variant">{operator.web}</span>
			</div>
		</div>
	</div>

	<div class="flex flex-col">
		<div class="bg-surface-container-lowest border border-outline-variant rounded-xl p-lg">
			<div class="space-y-sm">
				<div class="flex justify-between border-b border-outline-variant pb-2">
					<span class="text-on-surface-variant">{m.station_rate_off_peak()}</span>
					<span class="font-bold">{formatUpTo(rates.off_peak, 4)} €/kWh</span>
				</div>
				<div class="flex justify-between border-b border-outline-variant pb-2">
					<span class="text-on-surface-variant">{m.station_rate_flat()}</span>
					<span class="font-bold">{formatUpTo(rates.flat, 4)} €/kWh</span>
				</div>
				<div class="flex justify-between border-b border-outline-variant pb-2">
					<span class="text-on-surface-variant">{m.station_rate_peak()}</span>
					<span class="font-bold">{formatUpTo(rates.peak, 4)} €/kWh</span>
				</div>
				<div class="flex justify-between border-b border-outline-variant pb-2">
					<span class="text-on-surface-variant">{m.station_rate_vat()}</span>
					<span class="font-bold">{rates.vat_percent}%</span>
				</div>
				<div class="flex justify-between border-outline-variant pb-2">
					<span class="text-on-surface-variant">{m.station_rate_preauth()}</span>
					<span class="font-bold">{rates.preauth_amount} €</span>
				</div>
			</div>
		</div>

		<div class="mt-sm flex gap-xs items-start px-xs pt-xs">
			<Icon name="infoFill" size={16} class="text-on-surface-variant mt-0.5" />
			<p class="font-label-sm text-on-surface-variant leading-snug">
				{m.station_rate_note_1()} <strong>{m.station_rate_note_2()}</strong>
				{m.station_rate_note_3()} <strong>{m.station_rate_note_4()}</strong>
				{m.station_rate_note_5()}
			</p>
		</div>
	</div>
</div>
