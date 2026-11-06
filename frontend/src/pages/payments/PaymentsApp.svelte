<!--
@component
Payment history of the eMSP dashboard: the payments the CSMS stored (committed, cancelled, failed, abandoned...) for a
range of days, without personal data. The rows come from GET /api/admin/payments, which asks the CSMS.
-->
<script lang="ts">
	import { onMount } from 'svelte';
	import { apiFetch } from '$lib/api';
	import { getPageData } from '$lib/bootstrap';
	import type { PaymentRecord, PaymentsPage, PaymentsResult } from '$lib/contract.gen';
	import AdminHeader from '$lib/components/AdminHeader.svelte';
	import Icon from '$lib/components/Icon.svelte';
	import { errorMessage } from '$lib/errors';
	import { m } from '$lib/paraglide/messages';
	import {
		DISPLAY_STATUSES,
		STATUS_CLASSES,
		STATUS_LABELS,
		defaultRange,
		displayStatus,
		errorText,
		formatCents,
		formatDateTime,
		formatEnergy,
		summarize,
		type DisplayStatus
	} from './payments';
	import { sessionUrl, transactionUrl } from '../session/session';

	const view = getPageData<PaymentsPage>();
	const range = defaultRange(view?.today ?? new Date().toISOString().slice(0, 10));

	let from = $state(range.from);
	let to = $state(range.to);
	let poolCode = $state('');
	let statusFilter = $state<DisplayStatus | ''>('');

	let result = $state<PaymentsResult | null>(null);
	let loading = $state(false);
	let error = $state('');
	let loadedAt = $state(Date.now()); // "Now" of the last answer, for the ABANDONED status

	let shown = $derived(
		(result?.payments ?? []).filter((p) => statusFilter === '' || displayStatus(p, loadedAt) === statusFilter)
	);
	let summary = $derived(summarize(result?.payments ?? [], loadedAt));

	const statusLabels = STATUS_LABELS;
	const statusClasses = STATUS_CLASSES;

	async function load(event?: SubmitEvent) {
		event?.preventDefault();
		if (!view?.configured) return;
		loading = true;
		error = '';
		try {
			const params = new URLSearchParams({ from, to });
			if (poolCode) params.set('pool_code', poolCode);
			result = await apiFetch<PaymentsResult>('GET', `/api/admin/payments?${params}`);
			loadedAt = Date.now();
		} catch (failure) {
			error = errorMessage(failure);
		} finally {
			loading = false;
		}
	}

	/** The page of the payment's session; by its transaction id alone when the payment has no connector (Payter). */
	function paymentSessionUrl(p: PaymentRecord): string {
		const transactionId = p.transaction_id ?? 0;
		return p.connector_id === null
			? transactionUrl(transactionId)
			: sessionUrl(p.pool_code, p.station_name, p.connector_id, transactionId);
	}

	function where(p: PaymentRecord): string {
		return p.connector_id === null ? p.station_name : `${p.station_name} [${p.connector_id}]`;
	}

	onMount(() => {
		void load();
	});

	const inputClass =
		'w-full bg-surface border border-outline-variant rounded-lg px-3 py-2 text-body-md focus:outline-none focus:border-primary focus:ring-1 focus:ring-primary transition-shadow';
	const labelClass = 'block text-label-sm font-bold text-on-surface-variant mb-1';
</script>

<div class="bg-background text-on-background min-h-screen pt-16 pb-10 font-body-md text-body-md antialiased">
	<AdminHeader
		title={m.payments_title()}
		icon="payments"
		back={{ href: '/emsp-dashboard', label: m.payments_back(), icon: 'adminPanelSettings' }}
	/>

	<main class="max-w-[1440px] mx-auto p-4 md:p-8 space-y-lg w-full">
		{#if !view}
			<p class="text-error text-center py-10" role="alert">{m.payments_unavailable()}</p>
		{:else if !view.configured}
			<p class="text-error text-center py-10" role="alert">{m.payments_not_configured()}</p>
		{:else}
			<section class="bg-surface-container-lowest border border-outline-variant rounded-xl shadow-sm p-4 mb-8">
				<form onsubmit={load} class="flex flex-col md:flex-row gap-4 items-end">
					<div class="w-full md:flex-1">
						<label for="payments-from" class={labelClass}>{m.payments_from()}</label>
						<input type="date" id="payments-from" required max={to} class={inputClass} bind:value={from} />
					</div>
					<div class="w-full md:flex-1">
						<label for="payments-to" class={labelClass}>{m.payments_to()}</label>
						<input type="date" id="payments-to" required min={from} max={view.today} class={inputClass} bind:value={to} />
					</div>
					<div class="w-full md:flex-1">
						<label for="payments-pool" class={labelClass}>{m.payments_pool()}</label>
						<select id="payments-pool" class={inputClass} value={poolCode} onchange={(e) => (poolCode = e.currentTarget.value)}>
							<option value="">{m.payments_all_pools()}</option>
							{#each view.pools as pool (pool.pool_code)}
								<option value={String(pool.pool_code)}>{pool.pool_code}{pool.cpo_name ? ` · ${pool.cpo_name}` : ''}</option>
							{/each}
						</select>
					</div>
					<button
						type="submit"
						disabled={loading}
						class="w-full md:w-auto bg-primary hover:bg-on-primary-fixed-variant text-on-primary font-label-caps text-label-caps py-2 px-6 rounded-lg transition-colors flex items-center justify-center gap-2 h-[42px] disabled:opacity-50"
					>
						<Icon name={loading ? 'progressActivity' : 'manageSearch'} size={18} class={loading ? 'animate-spin' : ''} />
						{m.payments_search()}
					</button>
				</form>
				{#if error}
					<p class="text-error text-label-sm mt-sm" role="alert">{error}</p>
				{/if}
			</section>

			{#if result}
				<div class="flex flex-wrap gap-2 items-center mb-4" data-summary>
					<button
						type="button"
						onclick={() => (statusFilter = '')}
						aria-pressed={statusFilter === ''}
						class="font-label-caps text-label-caps px-3 py-1 rounded-full border border-outline-variant {statusFilter === ''
							? 'ring-2 ring-primary'
							: ''}"
					>
						{m.payments_all_statuses({ count: String(result.payments.length) })}
					</button>
					{#each DISPLAY_STATUSES as status (status)}
						{#if summary.counts[status] > 0}
							<button
								type="button"
								data-status-filter={status}
								onclick={() => (statusFilter = statusFilter === status ? '' : status)}
								aria-pressed={statusFilter === status}
								class="font-label-caps text-label-caps px-3 py-1 rounded-full border border-outline-variant {statusClasses[status]} {statusFilter ===
								status
									? 'ring-2 ring-primary'
									: ''}"
							>
								{statusLabels[status]()} · {summary.counts[status]}
							</button>
						{/if}
					{/each}
					<span class="ml-auto text-label-sm text-on-surface-variant" data-committed>
						{m.payments_committed_total({ total: formatCents(summary.committedCents) })}
					</span>
				</div>

				{#if result.truncated}
					<p class="text-label-sm text-error mb-4" role="status">{m.payments_truncated({ count: String(result.payments.length) })}</p>
				{/if}

				<section class="bg-surface-container-lowest border border-outline-variant rounded-xl shadow-sm overflow-hidden">
					<div class="overflow-x-auto">
						<table class="w-full text-left border-collapse">
							<thead>
								<tr class="bg-surface-container-low border-b border-outline-variant text-on-surface-variant font-label-sm">
									<th class="p-3">{m.payments_col_date()}</th>
									<th class="p-3">{m.payments_col_station()}</th>
									<th class="p-3">{m.payments_col_platform()}</th>
									<th class="p-3">{m.payments_col_status()}</th>
									<th class="p-3 text-right">{m.payments_col_limit()}</th>
									<th class="p-3 text-right">{m.payments_col_amount()}</th>
									<th class="p-3">{m.payments_col_session()}</th>
									<th class="p-3">{m.payments_col_reference()}</th>
									<th class="p-3">{m.payments_col_error()}</th>
								</tr>
							</thead>
							<tbody class="divide-y divide-outline-variant">
								{#each shown as payment (payment.id)}
									{@const status = displayStatus(payment, loadedAt)}
									<tr class="hover:bg-surface-container-low transition-colors align-top" data-payment={payment.id}>
										<td class="p-3 whitespace-nowrap">{formatDateTime(payment.created_at)}</td>
										<td class="p-3">
											<div class="font-mono text-sm">{where(payment)}</div>
											<div class="text-label-sm text-on-surface-variant">
												{payment.pool_code}{payment.cpo_name ? ` · ${payment.cpo_name}` : ''}
											</div>
										</td>
										<td class="p-3 whitespace-nowrap">
											{payment.platform}
											{#if payment.category === 'TEST'}
												<span class="ml-1 font-label-caps text-label-caps px-1.5 py-0.5 rounded bg-surface-container-high">{m.payments_test()}</span>
											{/if}
										</td>
										<td class="p-3">
											<span class="font-label-caps text-label-caps px-2 py-1 rounded whitespace-nowrap {statusClasses[status]}" data-status>
												{statusLabels[status]()}
											</span>
										</td>
										<td class="p-3 text-right whitespace-nowrap">{formatCents(payment.limit)}</td>
										<td class="p-3 text-right whitespace-nowrap font-bold">{formatCents(payment.amount)}</td>
										<td class="p-3 whitespace-nowrap">
											{#if payment.transaction_id !== null}
												<a href={paymentSessionUrl(payment)} class="font-mono text-sm text-primary hover:underline" data-session-link
													>#{payment.transaction_id}</a
												>
												<div class="text-label-sm text-on-surface-variant">{formatEnergy(payment.session_energy)}</div>
											{:else}
												—
											{/if}
										</td>
										<td class="p-3 font-mono text-sm break-all">
											{payment.order ?? payment.terminal ?? '—'}
											{#if payment.card_brand}
												<div class="text-label-sm text-on-surface-variant font-body-md">{payment.card_brand}</div>
											{/if}
										</td>
										<td class="p-3 text-label-sm text-error" data-error>
											{errorText(payment)}
											{#if payment.error_at}
												<div class="text-on-surface-variant">{formatDateTime(payment.error_at)}</div>
											{/if}
										</td>
									</tr>
								{:else}
									<tr>
										<td colspan="9" class="p-10 text-center text-on-surface-variant">{m.payments_empty()}</td>
									</tr>
								{/each}
							</tbody>
						</table>
					</div>
				</section>
				{#if result.source}
					<p class="text-label-sm text-on-surface-variant mt-2">
						{m.payments_source({ source: result.source })}
						{#if result.unanswered.length > 0}
							<span class="text-error" data-unanswered>{m.payments_unanswered({ hosts: result.unanswered.join(', ') })}</span>
						{/if}
					</p>
				{/if}
			{/if}
		{/if}
	</main>
</div>
