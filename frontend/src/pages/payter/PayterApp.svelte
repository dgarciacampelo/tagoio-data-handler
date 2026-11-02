<!--
@component
Payter terminals of the eMSP dashboard: every terminal by pool with the stations it serves, the form to add or edit
one, and its deactivation. Each change reaches the Payter CSMS as a manage_payter_terminal event.
-->
<script lang="ts">
	import { apiFetch } from '$lib/api';
	import { getPageData } from '$lib/bootstrap';
	import type { PayterPage, PayterTerminal, PayterTerminalInput } from '$lib/contract.gen';
	import AdminHeader from '$lib/components/AdminHeader.svelte';
	import Icon from '$lib/components/Icon.svelte';
	import { errorMessage } from '$lib/errors';
	import { m } from '$lib/paraglide/messages';
	import { formStations, movedStations, otherSharedTerminal, poolGroups, serialPattern } from './terminals';

	const view = getPageData<PayterPage>();
	const pools = view?.pools ?? [];

	let terminals = $state<PayterTerminal[]>(view?.terminals ?? []);
	let groups = $derived(poolGroups(terminals, pools));

	let serial = $state('');
	let poolCode = $state('');
	let shared = $state(false);
	let selected = $state<string[]>([]);
	let extra = $state('');
	let editing = $state<string | null>(null); // Serial number of the terminal in the form
	let busy = $state(false);
	let error = $state('');
	let notice = $state('');

	// The station picker: the pool's stations known to the service, plus those the edited terminal already serves
	let stationOptions = $derived.by(() => {
		const known = pools.find((p) => String(p.pool_code) === poolCode.trim())?.stations ?? [];
		return [...new Set([...known, ...selected])].sort();
	});

	function resetForm() {
		serial = '';
		poolCode = '';
		shared = false;
		selected = [];
		extra = '';
		editing = null;
	}

	function edit(terminal: PayterTerminal) {
		serial = terminal.serial_number;
		poolCode = String(terminal.pool_code);
		shared = terminal.is_pool_shared;
		selected = [...terminal.linked_stations];
		extra = '';
		editing = terminal.serial_number;
		error = '';
		notice = '';
		window.scrollTo({ top: 0, behavior: 'smooth' });
	}

	async function save(event: SubmitEvent) {
		event.preventDefault();
		error = '';
		notice = '';
		const target = serial.trim().toUpperCase();
		const pool = Number(poolCode.trim());
		const stations = shared ? [] : formStations(selected, extra);

		if (!shared && stations.length === 0) {
			error = m.payter_no_stations();
			return;
		}
		if (shared && otherSharedTerminal(terminals, target, pool)) {
			error = m.payter_shared_exists({ pool: String(pool) });
			return;
		}
		const moves = movedStations(terminals, target, pool, stations);
		if (moves.length > 0 && !confirm(m.payter_move_confirm({ moves: moves.map((x) => `${x.station} (${x.from})`).join(', ') }))) {
			return;
		}

		busy = true;
		try {
			const body: PayterTerminalInput = { pool_code: pool, is_pool_shared: shared, linked_stations: stations };
			terminals = await apiFetch<PayterTerminal[]>('PUT', `/api/admin/payter-terminals/${encodeURIComponent(target)}`, body);
			notice = m.payter_saved({ serial: target });
			resetForm();
		} catch (failure) {
			error = errorMessage(failure);
		} finally {
			busy = false;
		}
	}

	async function deactivate(terminal: PayterTerminal) {
		if (!confirm(m.payter_deactivate_confirm({ serial: terminal.serial_number }))) return;
		busy = true;
		error = '';
		notice = '';
		try {
			terminals = await apiFetch<PayterTerminal[]>('DELETE', `/api/admin/payter-terminals/${encodeURIComponent(terminal.serial_number)}`);
			if (editing === terminal.serial_number) resetForm();
		} catch (failure) {
			error = errorMessage(failure);
		} finally {
			busy = false;
		}
	}

	const inputClass =
		'w-full bg-surface border border-outline-variant rounded-lg px-3 py-2 text-body-md focus:outline-none focus:border-primary focus:ring-1 focus:ring-primary transition-shadow';
	const labelClass = 'block text-label-sm font-bold text-on-surface-variant mb-1';
</script>

<div class="bg-background text-on-background min-h-screen pt-16 pb-10 font-body-md text-body-md antialiased">
	<AdminHeader
		title={m.payter_title()}
		icon="creditCard"
		back={{ href: '/emsp-dashboard', label: m.payter_back(), icon: 'adminPanelSettings' }}
	/>

	<main class="max-w-[1440px] mx-auto p-4 md:p-8 space-y-lg w-full">
		{#if !view}
			<p class="text-error text-center py-10" role="alert">{m.payter_unavailable()}</p>
		{:else}
			<section class="bg-surface-container-lowest border border-outline-variant rounded-xl shadow-sm p-4 mb-8">
				<h2 class="text-title-md font-bold text-on-surface mb-4 flex items-center gap-2">
					<Icon name="addCircle" class="text-primary" />
					{m.payter_form_title()}
				</h2>
				<form onsubmit={save} class="flex flex-col gap-4">
					<div class="flex flex-col md:flex-row gap-4">
						<div class="w-full md:flex-1">
							<label for="payter-serial" class={labelClass}>{m.payter_serial()}</label>
							<input
								type="text"
								id="payter-serial"
								required
								pattern={serialPattern.source}
								title={m.payter_serial_hint()}
								readonly={editing !== null}
								class="{inputClass} uppercase read-only:bg-surface-container-low"
								placeholder="P6X20222101349"
								bind:value={serial}
								oninput={() => (serial = serial.toUpperCase())}
							/>
						</div>
						<div class="w-full md:flex-1">
							<label for="payter-pool" class={labelClass}>{m.payter_pool()}</label>
							<input
								type="text"
								id="payter-pool"
								required
								pattern={'\\d{6}'}
								inputmode="numeric"
								list="payter-pools"
								class={inputClass}
								placeholder="221006"
								bind:value={poolCode}
							/>
							<datalist id="payter-pools">
								{#each pools as pool (pool.pool_code)}
									<option value={String(pool.pool_code)}>{pool.cpo_name}</option>
								{/each}
							</datalist>
						</div>
					</div>

					<label class="flex items-center gap-2 text-body-md text-on-surface">
						<input type="checkbox" bind:checked={shared} class="accent-primary size-4" />
						{m.payter_shared()}
					</label>

					{#if !shared}
						<fieldset class="flex flex-col gap-2">
							<legend class={labelClass}>{m.payter_stations()}</legend>
							{#if stationOptions.length > 0}
								<p class="text-label-sm text-on-surface-variant">{m.payter_stations_known()}</p>
								<div class="flex flex-wrap gap-x-6 gap-y-2">
									{#each stationOptions as station (station)}
										<label class="flex items-center gap-2 font-mono text-body-md">
											<input type="checkbox" value={station} bind:group={selected} class="accent-primary size-4" />
											{station}
										</label>
									{/each}
								</div>
							{/if}
							<label for="payter-extra" class="text-label-sm text-on-surface-variant mt-2">{m.payter_stations_extra()}</label>
							<input type="text" id="payter-extra" class={inputClass} placeholder="22V20537CCPOS" bind:value={extra} />
						</fieldset>
					{/if}

					<div class="flex flex-col sm:flex-row gap-3 items-start sm:items-center">
						<button
							type="submit"
							disabled={busy}
							class="w-full sm:w-auto bg-primary hover:bg-on-primary-fixed-variant text-on-primary font-label-caps text-label-caps py-2 px-6 rounded-lg transition-colors flex items-center justify-center gap-2 h-[42px] disabled:opacity-50"
						>
							<Icon name="checkCircle" size={18} />
							{m.payter_save()}
						</button>
						{#if editing}
							<button type="button" onclick={resetForm} class="text-on-surface-variant hover:text-primary font-label-sm">
								{m.payter_cancel_edit()}
							</button>
						{/if}
					</div>
				</form>
				{#if error}
					<p class="text-error text-label-sm mt-sm whitespace-pre-line" role="alert">{error}</p>
				{/if}
				{#if notice}
					<p class="text-primary text-label-sm mt-sm" role="status">{notice}</p>
				{/if}
			</section>

			{#if groups.length === 0}
				<div class="text-center text-on-surface-variant py-10">
					<Icon name="creditCard" size={48} class="mb-2 opacity-50" />
					<p>{m.payter_empty()}</p>
				</div>
			{:else}
				{#each groups as group (group.poolCode)}
					<section class="mb-8" data-pool={group.poolCode}>
						<h2 class="text-title-lg font-bold text-on-surface flex items-center gap-2 border-b border-outline-variant pb-2 mb-4">
							<Icon name="evStation" class="text-primary" />
							{m.payter_pool_heading({ pool: String(group.poolCode), cpo: group.cpoName })}
						</h2>
						<div class="grid grid-cols-1 lg:grid-cols-2 gap-4">
							{#each group.terminals as terminal (terminal.serial_number)}
								<article
									data-terminal={terminal.serial_number}
									class="flex flex-col gap-3 border border-outline-variant rounded-xl p-4 bg-surface shadow-sm {terminal.is_active ? '' : 'opacity-60'}"
								>
									<div class="flex items-center justify-between gap-2">
										<h3 class="font-headline-md text-on-surface font-mono break-all">{terminal.serial_number}</h3>
										<span
											class="font-label-caps text-label-caps px-2 py-1 rounded border border-outline-variant whitespace-nowrap {terminal.is_active
												? 'bg-primary-container text-on-primary-container'
												: 'bg-surface-container-high text-on-surface-variant'}"
										>
											{terminal.is_active ? m.payter_active() : m.payter_inactive()}
										</span>
									</div>
									{#if terminal.is_active}
										<div class="flex flex-wrap gap-xs" data-stations>
											{#if terminal.is_pool_shared}
												<span class="text-body-md text-on-surface-variant">{m.payter_serves_all()}</span>
											{:else}
												{#each terminal.linked_stations as station (station)}
													<span class="font-mono text-label-sm px-2 py-1 rounded bg-surface-container-low border border-outline-variant">{station}</span>
												{/each}
											{/if}
										</div>
									{/if}
									<p class="text-label-sm text-on-surface-variant">{m.payter_updated({ date: terminal.updated_at })}</p>
									<div class="flex gap-3">
										<button
											type="button"
											onclick={() => edit(terminal)}
											disabled={busy}
											class="border border-outline hover:bg-surface-container-low text-primary font-label-caps text-label-caps py-1.5 px-4 rounded-lg transition-colors disabled:opacity-50"
										>
											{terminal.is_active ? m.payter_edit() : m.payter_reactivate()}
										</button>
										{#if terminal.is_active}
											<button
												type="button"
												onclick={() => deactivate(terminal)}
												disabled={busy}
												class="text-on-surface-variant hover:text-error font-label-caps text-label-caps py-1.5 px-4 rounded-lg transition-colors flex items-center gap-1 disabled:opacity-50"
											>
												<Icon name="delete" size={18} />
												{m.payter_deactivate()}
											</button>
										{/if}
									</div>
								</article>
							{/each}
						</div>
					</section>
				{/each}
			{/if}
		{/if}
	</main>
</div>
