<!--
@component
History of the finished sessions: sorted on the server by any column (dates sort as dates), filtered by period and
text, paged, with the totals of the filter (which replace the former expense calculator) and a CSV export.
-->
<script lang="ts">
	import { errorMessage } from '$lib/errors';
	import type { PanelHistoryPage, PanelHistorySort } from '$lib/contract.gen';
	import { m } from '$lib/paraglide/messages';
	import type { PanelClient } from './client';
	import { formatEnergy, formatLocalDateTime, formatMinutes, formatMoney, isoDate } from './panel';

	interface Props {
		client: PanelClient;
		notify: (message: string) => void;
	}

	let { client, notify }: Props = $props();

	// The server shows a CPO at most the last 90 days (HistoryWindowDays); an older history is exported on request.
	type Period = 7 | 30 | 60 | 90;
	const periods: { id: Period; label: () => string }[] = [
		{ id: 7, label: m.panel_history_7d },
		{ id: 30, label: m.panel_history_30d },
		{ id: 60, label: m.panel_history_60d },
		{ id: 90, label: m.panel_history_90d }
	];
	const columns: { id: PanelHistorySort; label: () => string; numeric: boolean }[] = [
		{ id: 'date', label: m.panel_history_col_date, numeric: false },
		{ id: 'station', label: m.panel_history_col_station, numeric: false },
		{ id: 'card', label: m.panel_history_col_card, numeric: false },
		{ id: 'minutes', label: m.panel_history_col_minutes, numeric: true },
		{ id: 'energy', label: m.panel_history_col_energy, numeric: true },
		{ id: 'cost', label: m.panel_history_col_cost, numeric: true }
	];

	let period = $state<Period>(30);
	let text = $state('');
	let sort = $state<PanelHistorySort>('date');
	let descending = $state(true);
	let page = $state(1);
	let result = $state.raw<PanelHistoryPage>();
	let loading = $state(false);

	const pages = $derived(result ? Math.max(1, Math.ceil(result.total / result.page_size)) : 1);

	/** The query string of the current filter (without the page). */
	function filter(): URLSearchParams {
		const params = new URLSearchParams({ sort, dir: descending ? 'desc' : 'asc' });
		const from = new Date();
		from.setDate(from.getDate() - (period - 1)); // Today included
		params.set('from', isoDate(from));
		if (text.trim()) params.set('q', text.trim());
		return params;
	}

	// Reloads when the filter, the order or the page change (the text waits for a pause in the typing).
	$effect(() => {
		const params = filter();
		params.set('page', String(page));
		const query = params.toString();
		const timer = setTimeout(async () => {
			loading = true;
			try {
				result = await client.call<PanelHistoryPage>('GET', `/history?${query}`);
			} catch (error) {
				notify(errorMessage(error));
			} finally {
				loading = false;
			}
		}, 250);
		return () => clearTimeout(timer);
	});

	function sortBy(column: PanelHistorySort) {
		if (sort === column) {
			descending = !descending;
		} else {
			sort = column;
			descending = column === 'date';
		}
		page = 1;
	}

	async function exportCSV() {
		try {
			const { blob, name } = await client.download(`/history.csv?${filter().toString()}`);
			const url = URL.createObjectURL(blob);
			const link = Object.assign(document.createElement('a'), { href: url, download: name });
			link.click();
			setTimeout(() => URL.revokeObjectURL(url), 10000);
		} catch (error) {
			notify(errorMessage(error));
		}
	}
</script>

<section class="rounded-xl border border-outline-variant bg-surface-container-lowest p-4 space-y-3">
	<div class="space-y-1">
		<h2 class="text-title-md font-semibold">{m.panel_history_title()}</h2>
		<p class="text-xs text-on-surface-variant">{m.panel_history_window_note()}</p>
	</div>
	<div class="flex flex-wrap items-center gap-2">
		<div class="flex gap-1" role="group" aria-label={m.panel_history_title()}>
			{#each periods as p (p.id)}
				<button
					type="button"
					aria-pressed={period === p.id}
					class="rounded-full border px-3 py-1 text-xs {period === p.id
						? 'bg-on-surface text-surface border-on-surface'
						: 'border-outline-variant'}"
					onclick={() => {
						period = p.id;
						page = 1;
					}}>{p.label()}</button
				>
			{/each}
		</div>
		<input
			id="history-search"
			type="search"
			class="min-w-0 flex-1 basis-48 rounded-lg border border-outline-variant bg-surface-container-lowest px-3 py-1.5"
			placeholder={m.panel_history_search()}
			aria-label={m.panel_history_search()}
			bind:value={text}
			oninput={() => (page = 1)}
		/>
		<button type="button" class="rounded-lg border border-outline-variant px-3 py-1.5 text-sm" onclick={exportCSV}>
			{m.panel_history_export()}
		</button>
	</div>

	<div class="overflow-x-auto rounded-lg border border-outline-variant {loading ? 'opacity-60' : ''}">
		<table class="w-full text-sm">
			<thead class="bg-surface-container-low text-xs text-on-surface-variant">
				<tr>
					{#each columns as column (column.id)}
						<th
							class="px-3 py-2 font-semibold whitespace-nowrap {column.numeric ? 'text-right' : 'text-left'}"
							aria-sort={sort === column.id ? (descending ? 'descending' : 'ascending') : undefined}
						>
							<button type="button" class="inline-flex items-center gap-1" onclick={() => sortBy(column.id)}>
								{column.label()}
								<span aria-hidden="true">{sort === column.id ? (descending ? '▼' : '▲') : '↕'}</span>
							</button>
						</th>
					{/each}
				</tr>
			</thead>
			<tbody>
				{#if result && result.items.length > 0}
					{#each result.items as item (item.transaction_id + item.station + item.started_at)}
						<tr class="border-t border-outline-variant">
							<td class="px-3 py-2 whitespace-nowrap">{formatLocalDateTime(item.started_at)}</td>
							<td class="px-3 py-2 whitespace-nowrap font-mono text-xs">{item.station}</td>
							<td class="px-3 py-2 whitespace-nowrap">{item.card}</td>
							<td class="px-3 py-2 whitespace-nowrap text-right tabular-nums">{formatMinutes(item.minutes)}</td>
							<td class="px-3 py-2 whitespace-nowrap text-right tabular-nums">{formatEnergy(item.energy_kwh)}</td>
							<td class="px-3 py-2 whitespace-nowrap text-right tabular-nums">{formatMoney(item.cost)}</td>
						</tr>
					{/each}
				{:else if result}
					<tr><td colspan="6" class="px-3 py-4 text-on-surface-variant">{m.panel_history_empty()}</td></tr>
				{/if}
			</tbody>
			{#if result && result.total > 0}
				<tfoot class="bg-surface-container-low font-semibold">
					<tr>
						<td class="px-3 py-2" colspan="3">{m.panel_history_total({ count: result.totals.sessions })}</td>
						<td class="px-3 py-2 text-right tabular-nums whitespace-nowrap">{formatMinutes(result.totals.minutes)}</td>
						<td class="px-3 py-2 text-right tabular-nums whitespace-nowrap">{formatEnergy(result.totals.energy_kwh)}</td>
						<td class="px-3 py-2 text-right tabular-nums whitespace-nowrap">{formatMoney(result.totals.cost)}</td>
					</tr>
				</tfoot>
			{/if}
		</table>
	</div>

	<div class="flex flex-wrap items-center justify-between gap-2 text-sm text-on-surface-variant">
		<span>{m.panel_history_page({ page, pages })}</span>
		<span class="flex gap-2">
			<button type="button" class="rounded border border-outline-variant px-3 py-1 disabled:opacity-40" disabled={page <= 1} onclick={() => page--}>
				{m.panel_previous()}
			</button>
			<button type="button" class="rounded border border-outline-variant px-3 py-1 disabled:opacity-40" disabled={page >= pages} onclick={() => page++}>
				{m.panel_next()}
			</button>
		</span>
	</div>
</section>
