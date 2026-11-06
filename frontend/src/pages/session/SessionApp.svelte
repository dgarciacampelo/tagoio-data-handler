<!--
@component
Page of a finished charging session: its summary and figures, the cost of each tariff band against the CSMS's cost,
the power and energy chart of its email, its payment (asked to the CSMS once the page shows) with what the card was
charged against that cost, and every meter value of its XLSX audit. When the address only gives a transaction id
shared by several charge points, it lists them.
-->
<script lang="ts">
	import { onMount } from 'svelte';
	import { apiFetch } from '$lib/api';
	import { getPageData } from '$lib/bootstrap';
	import type { SessionDetail, SessionPage, SessionPayments } from '$lib/contract.gen';
	import AdminHeader from '$lib/components/AdminHeader.svelte';
	import Icon from '$lib/components/Icon.svelte';
	import type { IconName } from '$lib/icons';
	import { errorMessage } from '$lib/errors';
	import { formatFixed } from '$lib/format';
	import { m } from '$lib/paraglide/messages';
	import { STATUS_CLASSES, STATUS_LABELS, displayStatus, errorText, formatCents, formatDateTime } from '../payments/payments';
	import SessionChart from './SessionChart.svelte';
	import {
		bandColors,
		bandLabels,
		bandShare,
		chargeComparison,
		chargeLabel,
		costCheckLabels,
		formatDuration,
		formatInstant,
		formatKwh,
		formatMeanPrice,
		formatMeterTime,
		formatPower,
		stopMotiveLabel
	} from './session';

	const view = getPageData<SessionPage>();
	const session: SessionDetail | null = view?.session ?? null;

	let payments = $state<SessionPayments | null>(null);
	let paymentsError = $state('');
	let paymentsLoadedAt = $state(Date.now());
	let showMeterValues = $state(false);

	const back = session
		? { href: session.audits_url, label: m.session_back(), icon: 'plumbing' as IconName }
		: { href: '/emsp-dashboard/audits', label: m.session_back(), icon: 'plumbing' as IconName };

	const kpis = session
		? [
				{ label: m.session_kpi_energy(), icon: 'bolt', value: formatKwh(session.energy_wh) },
				{ label: m.session_kpi_cost(), icon: 'euroSymbol', value: `${formatFixed(session.cost, 2)} €` },
				{ label: m.session_kpi_duration(), icon: 'timer', value: formatDuration(session.duration_seconds) },
				{ label: m.session_kpi_mean_power(), icon: 'speed', value: formatPower(session.mean_power_w), hint: m.session_kpi_mean_power_hint() },
				{ label: m.session_kpi_peak_power(), icon: 'batteryChargingFull', value: formatPower(session.peak_power_w) },
				{ label: m.session_kpi_idle(), icon: 'hourglassEmpty', value: formatDuration(session.idle_seconds), hint: m.session_kpi_idle_hint() },
				{ label: m.session_kpi_price(), icon: 'receiptLong', value: formatMeanPrice(session) },
				{ label: m.session_kpi_meter_values(), icon: 'tableChart', value: String(session.meter_value_count) }
			] satisfies { label: string; icon: IconName; value: string; hint?: string }[]
		: [];

	const checkClasses = {
		ok: 'bg-status-available/30 text-on-surface',
		capped: 'bg-status-charging/40 text-on-surface',
		mismatch: 'bg-error-container text-error'
	} as const;

	onMount(() => {
		if (!session) return;
		apiFetch<SessionPayments>('GET', session.payments_url)
			.then((answer) => {
				payments = answer;
				paymentsLoadedAt = Date.now();
			})
			.catch((failure) => (paymentsError = errorMessage(failure)));
	});

	const card = 'bg-surface-container-lowest border border-outline-variant rounded-xl shadow-sm';
	const cardHeading = 'p-4 border-b border-outline-variant bg-surface flex justify-between items-center gap-4';
	const title = 'text-title-lg font-bold text-on-surface flex items-center gap-2';
</script>

<div class="bg-background text-on-background min-h-screen pt-16 pb-10 font-body-md text-body-md antialiased">
	<AdminHeader title={m.session_title()} icon="evStation" {back} />

	<main class="max-w-[1440px] mx-auto p-4 md:p-8 space-y-6 w-full">
		{#if !view}
			<p class="text-error text-center py-10" role="alert">{m.session_unavailable()}</p>
		{:else if !session}
			<section class={card} data-matches>
				<div class={cardHeading}>
					<h1 class={title}>
						<Icon name="manageSearch" class="text-primary" />
						{m.session_matches_heading({ id: String(view.transaction_id) })}
					</h1>
				</div>
				{#if view.matches.length === 0}
					<p class="p-10 text-center text-on-surface-variant">{m.session_matches_empty({ id: String(view.transaction_id) })}</p>
				{:else}
					<p class="px-4 pt-4 text-on-surface-variant text-label-sm">{m.session_matches_hint()}</p>
					<ul class="divide-y divide-outline-variant">
						<!-- Unkeyed: the history can hold two rows of one session, and the list never changes -->
						{#each view.matches as match}
							<li>
								<a href={match.session_url} class="flex flex-wrap justify-between gap-2 p-4 hover:bg-surface-container-low transition-colors">
									<span>
										<span class="font-bold text-primary">{match.pool_code} / {match.station_name}</span>
										<span class="text-on-surface-variant text-sm">· {m.session_connector({ id: String(match.connector_id) })} · {match.card_alias}</span>
									</span>
									<span class="text-on-surface-variant text-sm">{match.start_date} · {match.time_band} · {formatFixed(match.total_energy_kwh, 3)} kWh · {formatFixed(match.cost, 2)} €</span>
								</a>
							</li>
						{/each}
					</ul>
				{/if}
			</section>
		{:else}
			<!-- Summary -->
			<section class="{card} p-4 md:p-6 space-y-4" data-summary>
				<div class="flex flex-wrap justify-between items-start gap-4">
					<div>
						<h1 class="text-headline-md font-headline-md font-bold text-on-surface">{m.session_heading({ id: String(session.transaction_id) })}</h1>
						<p class="text-on-surface-variant">
							{session.pool_code}{session.cpo_name ? ` · ${session.cpo_name}` : ''} · <span class="font-mono">{session.station_name}</span> · {m.session_connector({
								id: String(session.connector_id)
							})}
						</p>
					</div>
					<a
						href={session.export_url}
						class="inline-flex items-center justify-center bg-surface-container-highest hover:bg-outline-variant text-primary font-label-sm py-2 px-3 rounded-lg border border-outline-variant transition-colors gap-1"
						title={m.audits_excel_hint()}
					>
						<Icon name="tableChart" size={18} />
						{m.session_excel()}
					</a>
				</div>
				<dl class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-x-8 gap-y-3">
					<div>
						<dt class="text-label-sm text-on-surface-variant">{m.session_field_card()}</dt>
						<dd class="font-bold">{session.card_alias || '—'}</dd>
					</div>
					<div>
						<dt class="text-label-sm text-on-surface-variant">{m.session_field_start()}</dt>
						<dd data-started>{session.started_at ? formatInstant(session.started_at) : session.start_date}</dd>
					</div>
					<div>
						<dt class="text-label-sm text-on-surface-variant">{m.session_field_end()}</dt>
						<dd data-ended>{formatInstant(session.ended_at)}</dd>
					</div>
					<div>
						<dt class="text-label-sm text-on-surface-variant">{m.session_field_stop()}</dt>
						<dd data-stop-motive>{stopMotiveLabel(session.stop_motive)}</dd>
					</div>
					<div>
						<dt class="text-label-sm text-on-surface-variant">{m.session_field_last_delivery()}</dt>
						<dd>{formatInstant(session.last_delivery_at)}</dd>
					</div>
					<div>
						<dt class="text-label-sm text-on-surface-variant">{m.audits_col_period()}</dt>
						<dd class="font-mono">{session.start_date} · {session.time_band}</dd>
					</div>
				</dl>
			</section>

			<!-- Figures -->
			<section class="grid grid-cols-2 md:grid-cols-4 gap-4" data-kpis>
				{#each kpis as kpi (kpi.label)}
					<div class="{card} p-4 flex flex-col gap-1" title={kpi.hint}>
						<div class="flex justify-between items-center text-on-surface-variant">
							<span class="font-label-sm text-label-sm uppercase tracking-wider">{kpi.label}</span>
							<Icon name={kpi.icon} size={20} />
						</div>
						<span class="font-display-metrics text-xl md:text-2xl text-on-surface whitespace-nowrap">{kpi.value}</span>
					</div>
				{/each}
			</section>

			<!-- Chart -->
			<section class={card}>
				<div class={cardHeading}>
					<h2 class={title}>
						<Icon name="bolt" class="text-primary" />
						{m.session_chart_heading()}
					</h2>
				</div>
				<div class="p-4">
					{#if session.series}
						<SessionChart series={session.series} />
						<p class="text-label-sm text-on-surface-variant mt-2">{m.session_chart_hint()}</p>
					{:else}
						<p class="py-8 text-center text-on-surface-variant" data-no-chart>
							{session.telemetry_purged
								? m.session_chart_purged()
								: session.meter_value_count === 0
									? m.session_chart_no_meter_values()
									: m.session_chart_none()}
						</p>
					{/if}
				</div>
			</section>

			<div class="grid grid-cols-1 lg:grid-cols-2 gap-6">
				<!-- Tariff bands -->
				<section class="{card} overflow-hidden">
					<div class={cardHeading}>
						<h2 class={title}>
							<Icon name="euroSymbol" class="text-primary" />
							{m.session_bands_heading()}
						</h2>
					</div>
					<table class="w-full text-left border-collapse" data-bands>
						<thead>
							<tr class="bg-surface-container-low border-b border-outline-variant text-on-surface-variant font-label-sm">
								<th class="p-3">{m.session_band_col()}</th>
								<th class="p-3 text-right">{m.session_band_energy()}</th>
								<th class="p-3 text-right">{m.session_band_rate()}</th>
								<th class="p-3 text-right">{m.session_band_cost()}</th>
								<th class="p-3 text-right">{m.session_band_share()}</th>
							</tr>
						</thead>
						<tbody class="divide-y divide-outline-variant">
							{#each session.bands as band (band.band)}
								<tr>
									<td class="p-3"><span class="px-2 py-0.5 rounded font-bold {bandColors[band.band]}">{bandLabels[band.band]()}</span></td>
									<td class="p-3 text-right">{band.energy_wh} Wh</td>
									<td class="p-3 text-right font-mono">{formatFixed(band.rate, 4)} €/kWh</td>
									<td class="p-3 text-right">{formatFixed(band.cost, 4)} €</td>
									<td class="p-3 text-right text-on-surface-variant">{bandShare(session, band.energy_wh)}</td>
								</tr>
							{/each}
						</tbody>
						<tfoot class="border-t-2 border-outline">
							<tr>
								<td class="p-3 font-bold" colspan="3">{m.session_bands_total()}</td>
								<td class="p-3 text-right font-bold">{formatFixed(session.evaluated_cost, 4)} €</td>
								<td></td>
							</tr>
							<tr>
								<td class="p-3 font-bold" colspan="3">{m.session_cost_charged()}</td>
								<td class="p-3 text-right font-bold">{formatFixed(session.cost, 2)} €</td>
								<td></td>
							</tr>
							<tr>
								<td colspan="5" class="p-3">
									<span class="inline-block px-2 py-1 rounded text-label-sm font-bold {checkClasses[session.cost_check]}" data-cost-check={session.cost_check}>
										{costCheckLabels[session.cost_check]()}
									</span>
								</td>
							</tr>
						</tfoot>
					</table>
				</section>

				<!-- Payment -->
				<section class="{card} overflow-hidden" data-payments>
					<div class={cardHeading}>
						<h2 class={title}>
							<Icon name="payments" class="text-primary" />
							{m.session_payments_heading()}
						</h2>
					</div>
					<div class="p-4 space-y-3">
						{#if paymentsError}
							<p class="text-error text-label-sm" role="alert">{paymentsError}</p>
						{:else if !payments}
							<p class="text-on-surface-variant flex items-center gap-2">
								<Icon name="progressActivity" size={18} class="animate-spin" />
								{m.session_payments_loading()}
							</p>
						{:else}
							{#each payments.payments as payment (payment.id)}
								{@const status = displayStatus(payment, paymentsLoadedAt)}
								{@const charge = chargeComparison(session, payment)}
								<dl class="grid grid-cols-2 gap-x-4 gap-y-2 border border-outline-variant rounded-lg p-3" data-payment={payment.id}>
									<div>
										<dt class="text-label-sm text-on-surface-variant">{m.payments_col_status()}</dt>
										<dd>
											<span class="font-label-caps text-label-caps px-2 py-1 rounded whitespace-nowrap {STATUS_CLASSES[status]}" data-status>
												{STATUS_LABELS[status]()}
											</span>
											{#if payment.category === 'TEST'}
												<span class="ml-1 font-label-caps text-label-caps px-1.5 py-0.5 rounded bg-surface-container-high">{m.payments_test()}</span>
											{/if}
										</dd>
									</div>
									<div>
										<dt class="text-label-sm text-on-surface-variant">{m.payments_col_platform()}</dt>
										<dd>{payment.platform}{payment.card_brand ? ` · ${payment.card_brand}` : ''}</dd>
									</div>
									<div>
										<dt class="text-label-sm text-on-surface-variant">{m.payments_col_limit()}</dt>
										<dd>{formatCents(payment.limit)}</dd>
									</div>
									<div>
										<dt class="text-label-sm text-on-surface-variant">{m.payments_col_amount()}</dt>
										<dd class="font-bold">{formatCents(payment.amount)}</dd>
									</div>
									<div>
										<dt class="text-label-sm text-on-surface-variant">{m.payments_col_date()}</dt>
										<dd>{formatDateTime(payment.created_at)}</dd>
									</div>
									<div>
										<dt class="text-label-sm text-on-surface-variant">{m.payments_col_reference()}</dt>
										<dd class="font-mono text-sm break-all">{payment.order ?? payment.terminal ?? '—'}</dd>
									</div>
									{#if charge}
										<div class="col-span-2">
											<span
												class="inline-block px-2 py-1 rounded text-label-sm font-bold {checkClasses[charge.check === 'ok' ? 'ok' : 'mismatch']}"
												data-charge-check={charge.check}
											>
												{chargeLabel(charge)}
											</span>
										</div>
									{/if}
									{#if errorText(payment)}
										<div class="col-span-2 text-label-sm text-error" data-error>
											{errorText(payment)}
											{#if payment.error_at}<span class="text-on-surface-variant"> · {formatDateTime(payment.error_at)}</span>{/if}
										</div>
									{/if}
								</dl>
							{:else}
								<p class="text-on-surface-variant" data-no-payment>{m.session_payments_none()}</p>
							{/each}
							{#if payments.source}
								<p class="text-label-sm text-on-surface-variant">{m.payments_source({ source: payments.source })}</p>
							{/if}
						{/if}
					</div>
				</section>
			</div>

			<!-- Meter values -->
			{#if session.meter_values.length > 0}
				<section class="{card} overflow-hidden">
					<div class={cardHeading}>
						<h2 class={title}>
							<Icon name="tableChart" class="text-primary" />
							{m.session_meter_heading({ count: String(session.meter_values.length) })}
						</h2>
						<button
							type="button"
							class="text-primary font-label-sm hover:underline"
							aria-expanded={showMeterValues}
							onclick={() => (showMeterValues = !showMeterValues)}
							data-toggle-meter-values
						>
							{showMeterValues ? m.session_meter_hide() : m.session_meter_show()}
						</button>
					</div>
					{#if showMeterValues}
						<div class="overflow-x-auto max-h-[70vh]">
							<table class="w-full text-left border-collapse text-sm" data-meter-values>
								<thead class="sticky top-0">
									<tr class="bg-surface-container-low border-b border-outline-variant text-on-surface-variant font-label-sm">
										<th class="p-2">{m.session_meter_col_time()}</th>
										<th class="p-2 text-right">{m.session_meter_col_meter()}</th>
										<th class="p-2 text-right">{m.session_meter_col_power()}</th>
										<th class="p-2 text-right">{m.session_meter_col_cost()}</th>
										<th class="p-2 text-right">{m.session_meter_col_evaluated()}</th>
										<th class="p-2">{m.session_meter_col_band()}</th>
										<th class="p-2 text-right">{m.session_meter_col_off_peak()}</th>
										<th class="p-2 text-right">{m.session_meter_col_flat()}</th>
										<th class="p-2 text-right">{m.session_meter_col_peak()}</th>
									</tr>
								</thead>
								<tbody class="divide-y divide-outline-variant font-mono">
									<!-- Unkeyed: meter values stored before the charge point was recorded can repeat a timestamp -->
									{#each session.meter_values as row}
										<tr class="hover:bg-surface-container-low">
											<td class="p-2 whitespace-nowrap" title={row.timestamp}>{formatMeterTime(row.timestamp)}</td>
											<td class="p-2 text-right">{row.meter_value}</td>
											<td class="p-2 text-right">{row.power}</td>
											<td class="p-2 text-right">{formatFixed(row.cost, 2)}</td>
											<td class="p-2 text-right">{formatFixed(row.evaluated_cost, 4)}</td>
											<td class="p-2">{row.band}</td>
											<td class="p-2 text-right">{row.off_peak_wh}</td>
											<td class="p-2 text-right">{row.flat_wh}</td>
											<td class="p-2 text-right">{row.peak_wh}</td>
										</tr>
									{/each}
								</tbody>
							</table>
						</div>
					{/if}
				</section>
			{/if}
		{/if}
	</main>
</div>
