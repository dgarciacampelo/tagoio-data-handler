<!--
@component
Session audits: the latest finished sessions (of every pool, or of one), with their energy and frozen rates per
tariff band, and the XLSX export of every meter value.
-->
<script lang="ts">
	import { getPageData } from '$lib/bootstrap';
	import type { AuditsPage } from '$lib/contract.gen';
	import AdminHeader from '$lib/components/AdminHeader.svelte';
	import Icon from '$lib/components/Icon.svelte';
	import { formatFixed } from '$lib/format';
	import { m } from '$lib/paraglide/messages';

	const view = getPageData<AuditsPage>();

	const bands = [
		{ key: 'off_peak', label: m.audits_off_peak, colors: 'bg-band-off-peak text-on-band-off-peak' },
		{ key: 'flat', label: m.audits_flat, colors: 'bg-band-flat text-on-band-flat' },
		{ key: 'peak', label: m.audits_peak, colors: 'bg-band-peak text-on-band-peak' }
	] as const;
</script>

<div class="bg-background text-on-background min-h-screen pt-16 pb-10 font-body-md text-body-md antialiased">
	<AdminHeader
		title={m.audits_title()}
		icon="plumbing"
		back={{ href: '/emsp-dashboard', label: m.audits_back(), icon: 'adminPanelSettings' }}
	/>

	<main class="max-w-[1440px] mx-auto p-4 md:p-8 space-y-lg w-full">
		<section class="bg-surface-container-lowest border border-outline-variant rounded-xl shadow-sm overflow-hidden">
			<div class="p-4 border-b border-outline-variant bg-surface flex justify-between items-center">
				<h2 class="text-title-lg font-bold text-on-surface flex items-center gap-2">
					<Icon name="manageSearch" class="text-primary" />
					{view?.pool_code ? m.audits_heading_pool({ pool: String(view.pool_code) }) : m.audits_heading()}
				</h2>
			</div>

			<div class="overflow-x-auto">
				<table class="w-full text-left border-collapse">
					<thead>
						<tr class="bg-surface-container-low border-b border-outline-variant text-on-surface-variant font-label-sm">
							<th class="p-4">{m.audits_col_transaction()}</th>
							<th class="p-4">{m.audits_col_station()}</th>
							<th class="p-4">{m.audits_col_period()}</th>
							<th class="p-4 text-right">{m.audits_col_energy()}</th>
							<th class="p-4 text-right">{m.audits_col_cost()}</th>
							<th class="p-4 text-center">{m.audits_col_actions()}</th>
						</tr>
					</thead>
					<tbody class="divide-y divide-outline-variant">
						{#if !view}
							<tr>
								<td colspan="6" class="p-10 text-center text-error">{m.audits_unavailable()}</td>
							</tr>
						{:else}
							<!-- Unkeyed: no session field is unique (transaction ids repeat across charge points, and the history
							     can hold two rows of one session), and the list never changes after the page loads. -->
							{#each view.sessions as session}
								<tr class="hover:bg-surface-container-low transition-colors">
									<td class="p-4 font-mono text-sm font-bold">{session.transaction_id}</td>
									<td class="p-4">
										<div class="font-bold text-on-surface">{session.pool_code} / {session.station_name}</div>
										<div class="text-on-surface-variant text-sm">
											{m.audits_connector({ id: session.connector_id, alias: session.card_alias })}
										</div>
									</td>
									<td class="p-4">
										<div>{session.start_date}</div>
										<div class="text-on-surface-variant text-sm font-mono">{session.time_band}</div>
									</td>
									<td class="p-4 text-right font-display-metrics text-primary">{formatFixed(session.total_energy_kwh, 3)} kWh</td>
									<td class="p-4 text-right font-display-metrics font-bold">{formatFixed(session.cost, 2)} €</td>
									<td class="p-4 text-center">
										<a
											href={session.export_url}
											class="inline-flex items-center justify-center bg-surface-container-highest hover:bg-outline-variant text-primary font-label-sm py-2 px-3 rounded-lg border border-outline-variant transition-colors gap-1"
											title={m.audits_excel_hint()}
										>
											<Icon name="tableChart" size={18} />
											{m.audits_excel()}
										</a>
									</td>
								</tr>
								<tr class="bg-surface-container-lowest border-b-2 border-outline">
									<td colspan="6" class="p-0">
										<div class="px-4 py-3 flex flex-wrap gap-4 text-sm justify-end bg-surface-container-lowest">
											{#each bands as band (band.key)}
												{@const energy = session[`${band.key}_wh`]}
												{@const rate = session[`rate_${band.key}`]}
												<div class="flex items-center gap-2 border border-outline-variant rounded px-3 py-1 {band.colors}">
													<span class="font-bold">{band.label()}</span>
													<span>{energy} Wh</span>
													<span class="opacity-50">|</span>
													<span class="font-mono">{formatFixed(rate, 3)} €/kWh</span>
												</div>
											{/each}
										</div>
									</td>
								</tr>
							{:else}
								<tr>
									<td colspan="6" class="p-10 text-center text-on-surface-variant">
										<Icon name="inbox" size={32} class="opacity-50 mb-2" />
										<p>{m.audits_empty()}</p>
									</td>
								</tr>
							{/each}
						{/if}
					</tbody>
				</table>
			</div>
		</section>
	</main>
</div>
