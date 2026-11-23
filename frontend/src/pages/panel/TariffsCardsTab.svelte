<!--
@component
"Tarifas y tarjetas" section of the management panel, in two columns: the RFID cards (created, changed and
deleted in place) on the left; the installation power, the rates and the diagram of the tariff hours on the right.
-->
<script lang="ts">
	import { errorMessage } from '$lib/errors';
	import type { PanelCard, PanelCardRequest, PanelOverview, PanelRates } from '$lib/contract.gen';
	import { m } from '$lib/paraglide/messages';
	import type { PanelClient } from './client';
	import { bandAt, bandFills, bandLabels, formatMoney, formatPower } from './panel';
	import { formatFixed } from '$lib/format';

	interface Props {
		client: PanelClient;
		overview: PanelOverview;
		onoverview: (overview: PanelOverview) => void;
		notify: (message: string) => void;
	}

	let { client, overview, onoverview, notify }: Props = $props();

	// region Cards

	let cards = $state.raw<PanelCard[]>([]);
	let cardSearch = $state('');
	let editing = $state<'new' | string | null>(null);
	let form = $state<PanelCardRequest>({ id: '', alias: '', email: '', linked_stations: [] });
	let armedDelete = $state('');
	let saving = $state(false);

	const visibleCards = $derived.by(() => {
		const q = cardSearch.trim().toLowerCase();
		return q ? cards.filter((c) => [c.id, c.alias, c.email].some((v) => v.toLowerCase().includes(q))) : cards;
	});

	async function loadCards() {
		try {
			cards = await client.call<PanelCard[]>('GET', '/cards');
		} catch (error) {
			notify(errorMessage(error));
		}
	}

	$effect(() => {
		void loadCards();
	});

	function startEdit(card?: PanelCard) {
		editing = card ? card.id : 'new';
		form = card
			? { id: card.id, alias: card.alias, email: card.email, linked_stations: [...card.linked_stations] }
			: { id: '', alias: '', email: '', linked_stations: [...overview.stations] };
	}

	function toggleStation(station: string) {
		form.linked_stations = form.linked_stations.includes(station)
			? form.linked_stations.filter((s) => s !== station)
			: [...form.linked_stations, station];
	}

	async function saveCard(event: SubmitEvent) {
		event.preventDefault();
		saving = true;
		const isNew = editing === 'new';
		try {
			if (isNew) {
				await client.call('POST', '/cards', form);
			} else {
				await client.call('PUT', `/cards/${encodeURIComponent(form.id)}`, form);
			}
			const alias = (form.alias || form.id).toUpperCase();
			notify(isNew ? m.panel_cards_created({ alias }) : m.panel_cards_updated({ alias }));
			editing = null;
			await loadCards();
		} catch (error) {
			notify(errorMessage(error));
		} finally {
			saving = false;
		}
	}

	async function deleteCard(card: PanelCard) {
		if (armedDelete !== card.id) {
			armedDelete = card.id;
			setTimeout(() => armedDelete === card.id && (armedDelete = ''), 4000);
			return;
		}
		armedDelete = '';
		try {
			await client.call('DELETE', `/cards/${encodeURIComponent(card.id)}`);
			notify(m.panel_cards_deleted({ alias: card.alias || card.id }));
			await loadCards();
		} catch (error) {
			notify(errorMessage(error));
		}
	}

	// endregion

	// region Power and rates

	let editingPower = $state(false);
	let powerKW = $state(0);
	let editingRates = $state(false);
	let rates = $state<PanelRates>({ off_peak: 0, flat: 0, peak: 0, vat_percent: 0, withholding: 0 });

	async function savePower() {
		saving = true;
		try {
			onoverview(await client.call<PanelOverview>('PUT', '/max-power', { max_power_w: Math.round(powerKW * 1000) }));
			notify(m.panel_power_saved({ power: formatPower(powerKW * 1000) }));
			editingPower = false;
		} catch (error) {
			notify(errorMessage(error));
		} finally {
			saving = false;
		}
	}

	async function saveRates() {
		saving = true;
		try {
			onoverview(await client.call<PanelOverview>('PUT', '/rates', rates));
			notify(m.panel_rates_saved());
			editingRates = false;
		} catch (error) {
			notify(errorMessage(error));
		} finally {
			saving = false;
		}
	}

	const rateRows = [
		{ key: 'off_peak', label: m.panel_band_off_peak, border: 'border-band-off-peak-strong' },
		{ key: 'flat', label: m.panel_band_flat, border: 'border-band-flat-strong' },
		{ key: 'peak', label: m.panel_band_peak, border: 'border-band-peak-strong' }
	] as const;

	// endregion

	// region Tariff hours diagram

	const chart = { left: 112, width: 400, row: 26 };
	const hourWidth = chart.width / 24;
	let nowHours = $state(new Date().getHours() + new Date().getMinutes() / 60);
	$effect(() => {
		const timer = setInterval(() => {
			const d = new Date();
			nowHours = d.getHours() + d.getMinutes() / 60;
		}, 60000);
		return () => clearInterval(timer);
	});

	// endregion
</script>

<div class="grid gap-4 lg:grid-cols-[minmax(0,1.45fr)_minmax(0,1fr)] items-start">
	<section class="rounded-xl border border-outline-variant bg-surface-container-lowest p-4 space-y-3 min-w-0">
		<div class="flex flex-wrap items-center justify-between gap-2">
			<h2 class="text-title-md font-semibold">
				{m.panel_cards_title()} <span class="font-normal text-on-surface-variant">· {cards.length}</span>
			</h2>
			<button type="button" class="rounded-lg bg-primary text-on-primary px-3 py-1.5 text-sm font-medium" onclick={() => startEdit()}>
				+ {m.panel_cards_new()}
			</button>
		</div>

		{#if editing !== null}
			<form class="rounded-lg border border-primary bg-primary-fixed/20 p-3 space-y-3" onsubmit={saveCard}>
				<h3 class="font-semibold">
					{editing === 'new' ? m.panel_cards_form_new() : m.panel_cards_form_edit({ alias: form.alias || form.id })}
				</h3>
				<div class="grid gap-3 sm:grid-cols-3">
					<label class="grid gap-1 text-xs text-on-surface-variant">
						{m.panel_cards_form_id()}
						<input id="card-id" class="rounded border border-outline-variant bg-surface-container-lowest px-2 py-1.5 text-sm text-on-surface font-mono" required readonly={editing !== 'new'} bind:value={form.id} />
					</label>
					<label class="grid gap-1 text-xs text-on-surface-variant">
						{m.panel_cards_form_alias()}
						<input id="card-alias" class="rounded border border-outline-variant bg-surface-container-lowest px-2 py-1.5 text-sm text-on-surface" maxlength="60" bind:value={form.alias} />
					</label>
					<label class="grid gap-1 text-xs text-on-surface-variant">
						{m.panel_cards_form_email()}
						<input id="card-email" type="email" class="rounded border border-outline-variant bg-surface-container-lowest px-2 py-1.5 text-sm text-on-surface" bind:value={form.email} />
					</label>
				</div>
				<fieldset class="space-y-1">
					<legend class="text-xs text-on-surface-variant">{m.panel_cards_form_stations()}</legend>
					<div class="flex flex-wrap gap-x-4 gap-y-1">
						{#each overview.stations as station (station)}
							<label class="inline-flex items-center gap-1.5 font-mono text-xs">
								<input type="checkbox" checked={form.linked_stations.includes(station)} onchange={() => toggleStation(station)} />
								{station}
							</label>
						{/each}
					</div>
				</fieldset>
				<div class="flex justify-end gap-2">
					<button type="button" class="rounded border border-outline-variant px-3 py-1.5 text-sm" onclick={() => (editing = null)}>
						{m.panel_cancel()}
					</button>
					<button type="submit" class="rounded bg-primary text-on-primary px-3 py-1.5 text-sm font-medium disabled:opacity-50" disabled={saving || form.linked_stations.length === 0}>
						{saving ? m.panel_saving() : editing === 'new' ? m.panel_cards_form_create() : m.panel_save()}
					</button>
				</div>
			</form>
		{/if}

		<input
			id="card-search"
			type="search"
			class="w-full rounded-lg border border-outline-variant bg-surface-container-lowest px-3 py-1.5"
			placeholder={m.panel_cards_search()}
			aria-label={m.panel_cards_search()}
			bind:value={cardSearch}
		/>
		<div class="overflow-x-auto rounded-lg border border-outline-variant">
			<table class="w-full text-sm">
				<thead class="bg-surface-container-low text-xs text-on-surface-variant">
					<tr>
						<th class="px-3 py-2 text-left">{m.panel_cards_col_id()}</th>
						<th class="px-3 py-2 text-left">{m.panel_cards_col_alias()}</th>
						<th class="px-3 py-2 text-left">{m.panel_cards_col_email()}</th>
						<th class="px-3 py-2 text-left">{m.panel_cards_col_stations()}</th>
						<th class="px-3 py-2 text-right whitespace-nowrap">{m.panel_cards_col_spent()}</th>
						<th class="px-3 py-2"></th>
					</tr>
				</thead>
				<tbody>
					{#each visibleCards as card (card.id)}
						<tr class="border-t border-outline-variant align-top">
							<td class="px-3 py-2 font-mono text-xs whitespace-nowrap">{card.id}</td>
							<td class="px-3 py-2">{card.alias}</td>
							<td class="px-3 py-2 break-all">{card.email || '—'}</td>
							<td class="px-3 py-2">
								{#if overview.stations.length > 0 && overview.stations.every((s) => card.linked_stations.includes(s))}
									<span class="rounded bg-surface-container px-1.5 py-0.5 text-xs">{m.panel_cards_all_stations()}</span>
								{:else}
									{#each card.linked_stations as s (s)}
										<span class="mr-1 inline-block rounded bg-surface-container px-1.5 py-0.5 font-mono text-xs">{s}</span>
									{/each}
								{/if}
							</td>
							<td class="px-3 py-2 text-right tabular-nums whitespace-nowrap">{formatMoney(card.spent_30_days)}</td>
							<td class="px-3 py-2 text-right whitespace-nowrap">
								<button type="button" class="rounded px-2 py-1 text-xs text-on-surface-variant hover:bg-surface-container" onclick={() => startEdit(card)}>
									{m.panel_edit()}
								</button>
								<button
									type="button"
									class="rounded px-2 py-1 text-xs hover:bg-surface-container {armedDelete === card.id ? 'text-error font-semibold' : 'text-on-surface-variant'}"
									onclick={() => deleteCard(card)}
								>
									{armedDelete === card.id ? m.panel_confirm_delete() : m.panel_delete()}
								</button>
							</td>
						</tr>
					{:else}
						<tr><td colspan="6" class="px-3 py-4 text-on-surface-variant">{m.panel_cards_empty()}</td></tr>
					{/each}
				</tbody>
			</table>
		</div>
	</section>

	<div class="space-y-4 min-w-0">
		<section class="rounded-xl border border-outline-variant bg-surface-container-lowest p-4 space-y-2">
			<div class="flex items-center justify-between gap-2">
				<h2 class="text-title-md font-semibold">{m.panel_power_title()}</h2>
				{#if !editingPower}
					<button
						type="button"
						class="rounded border border-outline-variant px-3 py-1 text-sm"
						onclick={() => {
							powerKW = overview.max_power_w / 1000;
							editingPower = true;
						}}>{m.panel_edit()}</button
					>
				{/if}
			</div>
			{#if editingPower}
				<div class="flex flex-wrap items-end gap-2">
					<label class="grid flex-1 gap-1 text-xs text-on-surface-variant">
						{m.panel_power_field()}
						<input id="power-kw" type="number" min="1" step="0.1" class="rounded border border-outline-variant px-2 py-1.5 text-sm text-on-surface" bind:value={powerKW} />
					</label>
					<button type="button" class="rounded border border-outline-variant px-3 py-1.5 text-sm" onclick={() => (editingPower = false)}>{m.panel_cancel()}</button>
					<button type="button" class="rounded bg-primary text-on-primary px-3 py-1.5 text-sm font-medium disabled:opacity-50" disabled={saving || !(powerKW >= 1)} onclick={savePower}>
						{saving ? m.panel_saving() : m.panel_save()}
					</button>
				</div>
			{:else}
				<p class="text-2xl font-semibold tabular-nums">
					{formatFixed(overview.max_power_w / 1000, 1)} <span class="text-sm font-normal text-on-surface-variant">{m.panel_power_contracted()}</span>
				</p>
				<p class="text-xs text-on-surface-variant">{m.panel_power_hint()}</p>
			{/if}
		</section>

		<section class="rounded-xl border border-outline-variant bg-surface-container-lowest p-4 space-y-3">
			<div class="flex items-center justify-between gap-2">
				<h2 class="text-title-md font-semibold">
					{m.panel_rates_title()} <span class="font-normal text-on-surface-variant text-sm">{m.panel_rates_vat_included()}</span>
				</h2>
				{#if !editingRates}
					<button
						type="button"
						class="rounded border border-outline-variant px-3 py-1 text-sm"
						onclick={() => {
							rates = { ...overview.rates };
							editingRates = true;
						}}>{m.panel_edit()}</button
					>
				{/if}
			</div>
			<div class="grid grid-cols-3 gap-2">
				{#each rateRows as row (row.key)}
					<div class="rounded-lg bg-surface-container-low border-t-4 {row.border} p-2 min-w-0">
						<p class="text-xs text-on-surface-variant">{row.label()}</p>
						{#if editingRates}
							<input id="rate-{row.key}" type="number" min="0" step="0.001" aria-label={row.label()} class="w-full rounded border border-outline-variant px-1.5 py-1 text-sm tabular-nums" bind:value={rates[row.key]} />
						{:else}
							<p class="text-lg font-semibold tabular-nums">
								{formatFixed(overview.rates[row.key], 3)} <span class="text-xs font-normal text-on-surface-variant">{m.panel_rates_unit()}</span>
							</p>
						{/if}
					</div>
				{/each}
			</div>
			<dl class="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 text-sm">
				<dt class="text-on-surface-variant">{m.panel_rates_vat()}</dt>
				<dd>
					{#if editingRates}
						<input id="rate-vat" type="number" min="0" max="30" step="0.1" class="w-20 rounded border border-outline-variant px-1.5 py-0.5" bind:value={rates.vat_percent} /> %
					{:else}
						{formatFixed(overview.rates.vat_percent, 0)} %
					{/if}
				</dd>
				<dt class="text-on-surface-variant">{m.panel_rates_withholding()}</dt>
				<dd>
					{#if editingRates}
						<input id="rate-withholding" type="number" min="0" step="1" class="w-20 rounded border border-outline-variant px-1.5 py-0.5" bind:value={rates.withholding} /> €
					{:else}
						{formatMoney(overview.rates.withholding)} <span class="text-on-surface-variant">{m.panel_rates_withholding_hint()}</span>
					{/if}
				</dd>
			</dl>
			{#if editingRates}
				<div class="flex justify-end gap-2">
					<button type="button" class="rounded border border-outline-variant px-3 py-1.5 text-sm" onclick={() => (editingRates = false)}>{m.panel_cancel()}</button>
					<button type="button" class="rounded bg-primary text-on-primary px-3 py-1.5 text-sm font-medium disabled:opacity-50" disabled={saving} onclick={saveRates}>
						{saving ? m.panel_saving() : m.panel_save()}
					</button>
				</div>
			{/if}
		</section>

		<section class="rounded-xl border border-outline-variant bg-surface-container-lowest p-4 space-y-2">
			<h2 class="text-title-md font-semibold">{m.panel_bands_title()}</h2>
			<svg viewBox="0 0 520 132" class="w-full h-auto" role="img" aria-label={m.panel_bands_title()}>
				{#each [{ label: m.panel_bands_weekdays(), y: 18, weekday: true }, { label: m.panel_bands_weekends(), y: 62, weekday: false }] as row (row.y)}
					<text x="0" y={row.y + 17} class="fill-on-surface-variant text-[11px]">{row.label}</text>
					{#each Array.from({ length: 24 }, (_, h) => h) as hour (hour)}
						<rect
							x={chart.left + hour * hourWidth + 0.5}
							y={row.y}
							width={hourWidth - 1}
							height={chart.row}
							rx="2"
							class={bandFills[row.weekday ? bandAt(overview.bands, hour) : 'off_peak']}
						/>
					{/each}
				{/each}
				{#each Array.from({ length: 13 }, (_, i) => i * 2) as hour (hour)}
					<text x={chart.left + hour * hourWidth} y="112" text-anchor="middle" class="fill-on-surface-variant text-[10px] tabular-nums">{hour}</text>
				{/each}
				<line x1={chart.left + nowHours * hourWidth} x2={chart.left + nowHours * hourWidth} y1="10" y2="96" class="stroke-on-surface" stroke-width="2" />
				<text x={chart.left + nowHours * hourWidth} y="128" text-anchor="middle" class="fill-on-surface text-[10px]">{m.panel_bands_now()}</text>
			</svg>
			<div class="flex flex-wrap gap-3 text-xs text-on-surface-variant">
				{#each ['off_peak', 'flat', 'peak'] as const as band (band)}
					<span class="inline-flex items-center gap-1">
						<svg width="10" height="10" aria-hidden="true"><rect width="10" height="10" rx="2" class={bandFills[band]} /></svg>
						{bandLabels[band]()}
					</span>
				{/each}
			</div>
			<p class="text-xs text-on-surface-variant">{m.panel_bands_note()}</p>
		</section>
	</div>
</div>
