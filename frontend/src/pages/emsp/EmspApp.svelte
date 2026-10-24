<!--
@component
eMSP dashboard: every pool with its stations and the status of their connectors, the QR poster of each station (and
of a station that has not reported yet), the link to the station's public page, and the station deletion.
-->
<script lang="ts">
	import { scale } from 'svelte/transition';
	import { apiFetch } from '$lib/api';
	import { getPageData } from '$lib/bootstrap';
	import type { EmspPage, EmspPool, StationLink } from '$lib/contract.gen';
	import AdminHeader from '$lib/components/AdminHeader.svelte';
	import Icon from '$lib/components/Icon.svelte';
	import { errorMessage } from '$lib/errors';
	import { m } from '$lib/paraglide/messages';
	import { statusStyle } from '$lib/status';
	import QrModal from './QrModal.svelte';

	const view = getPageData<EmspPage>()!;

	let pools = $state<EmspPool[]>(view.pools);
	let qrLink = $state.raw<StationLink | null>(null);
	let deleting = $state<string | null>(null); // "<pool>/<station>" being deleted

	let scaffoldPool = $state('');
	let scaffoldStation = $state('');
	let scaffoldError = $state('');

	async function openScaffold(event: SubmitEvent) {
		event.preventDefault();
		scaffoldError = '';
		const params = new URLSearchParams({ pool_code: scaffoldPool.trim(), station_name: scaffoldStation.trim() });
		try {
			qrLink = await apiFetch<StationLink>('GET', `/api/admin/station-link?${params}`);
		} catch (error) {
			scaffoldError = errorMessage(error);
		}
	}

	async function deleteStation(pool: EmspPool, station: string) {
		if (!confirm(m.emsp_delete_confirm({ station, pool: String(pool.pool_code) }))) return;
		deleting = `${pool.pool_code}/${station}`;
		try {
			await apiFetch('DELETE', `/api/admin/stations/${pool.pool_code}/${encodeURIComponent(station)}`);
			pool.stations = pool.stations.filter((s) => s.name !== station);
		} catch {
			alert(m.emsp_delete_error());
		} finally {
			deleting = null;
		}
	}

	const inputClass =
		'w-full bg-surface border border-outline-variant rounded-lg px-3 py-2 text-body-md focus:outline-none focus:border-primary focus:ring-1 focus:ring-primary transition-shadow';
</script>

<div class="bg-background text-on-background min-h-screen pt-16 pb-10 font-body-md text-body-md antialiased">
	<AdminHeader title={m.emsp_title()} icon="adminPanelSettings" />

	<main class="max-w-[1440px] mx-auto p-4 md:p-8 space-y-lg w-full">
		<section class="bg-surface-container-lowest border border-outline-variant rounded-xl shadow-sm p-4 mb-8">
			<h2 class="text-title-md font-bold text-on-surface mb-4 flex items-center gap-2">
				<Icon name="addCircle" class="text-primary" />
				{m.emsp_scaffold_title()}
			</h2>
			<form onsubmit={openScaffold} class="flex flex-col md:flex-row gap-4 items-end">
				<div class="w-full md:flex-[2]">
					<label for="scaffold-pool" class="block text-label-sm font-bold text-on-surface-variant mb-1">{m.emsp_scaffold_pool()}</label>
					<input
						type="text"
						id="scaffold-pool"
						pattern={'\\d{6}'}
						inputmode="numeric"
						required
						title={m.emsp_scaffold_pool_hint()}
						class={inputClass}
						placeholder={m.emsp_scaffold_pool_placeholder()}
						bind:value={scaffoldPool}
					/>
				</div>
				<div class="w-full md:flex-[3]">
					<label for="scaffold-station" class="block text-label-sm font-bold text-on-surface-variant mb-1">{m.emsp_scaffold_station()}</label>
					<input
						type="text"
						id="scaffold-station"
						required
						class={inputClass}
						placeholder={m.emsp_scaffold_station_placeholder()}
						bind:value={scaffoldStation}
					/>
				</div>
				<button
					type="submit"
					class="w-full md:w-auto bg-primary hover:bg-on-primary-fixed-variant text-on-primary font-label-caps text-label-caps py-2 px-6 rounded-lg transition-colors flex items-center justify-center gap-2 h-[42px] shrink-0"
				>
					<Icon name="qrCode2" size={18} />
					{m.emsp_generate_qr()}
				</button>
			</form>
			{#if scaffoldError}
				<p class="text-error text-label-sm mt-sm" role="alert">{scaffoldError}</p>
			{/if}
		</section>

		{#if pools.every((pool) => pool.stations.length === 0)}
			<div class="text-center text-on-surface-variant py-10">
				<Icon name="evStation" size={48} class="mb-2 opacity-50" />
				<p>{m.emsp_empty()}</p>
			</div>
		{:else}
			{#each pools as pool (pool.pool_code)}
				{#if pool.stations.length > 0}
					<section class="mb-8">
						<div
							class="flex flex-col sm:flex-row justify-between items-start sm:items-center border-b border-outline-variant pb-2 mb-4 gap-3"
						>
							<h2 class="text-title-lg font-bold text-on-surface flex items-center gap-2">
								<Icon name="evStation" class="text-primary" />
								{m.emsp_pool_title({ pool: String(pool.pool_code), cpo: pool.cpo_name })}
							</h2>
							<a
								href={pool.audits_url}
								class="border border-outline hover:bg-surface-container-low text-primary font-label-caps text-label-caps py-1.5 px-4 rounded-lg transition-colors flex items-center justify-center gap-2 text-center shrink-0"
							>
								<Icon name="receiptLong" size={18} />
								{m.emsp_view_sessions()}
							</a>
						</div>

						<div class="grid grid-cols-1 lg:grid-cols-2 gap-4">
							{#each pool.stations as station (station.name)}
								<div
									out:scale={{ duration: 300, start: 0.95 }}
									class="flex flex-col sm:flex-row items-center justify-between w-full border border-outline-variant rounded-xl p-4 gap-4 bg-surface shadow-sm"
								>
									<div class="flex flex-col gap-3 w-full" style="flex: 6;">
										<div class="flex items-center gap-2">
											<button
												type="button"
												onclick={() => deleteStation(pool, station.name)}
												disabled={deleting === `${pool.pool_code}/${station.name}`}
												class="text-on-surface-variant hover:text-error transition-colors p-1.5 rounded-full hover:bg-surface-container-high flex items-center justify-center opacity-80 hover:opacity-100 shrink-0 disabled:opacity-40 disabled:pointer-events-none"
												title={m.emsp_delete_station()}
												aria-label={m.emsp_delete_station()}
											>
												<Icon name="delete" size={20} />
											</button>
											<h3 class="font-headline-md text-on-surface break-all">{station.name}</h3>
										</div>

										<div class="flex flex-wrap items-center gap-xs w-full pl-10">
											{#each station.connectors as connector (connector.id)}
												{@const style = statusStyle(connector.status)}
												<span
													class="font-label-caps text-label-caps px-2 py-1 rounded text-black whitespace-nowrap shadow-sm border border-outline-variant flex items-center gap-xs {style.background}"
												>
													<span class="opacity-60 font-mono text-[11px]">C{connector.id}:</span>
													{style.label()}
												</span>
											{/each}
										</div>
									</div>

									<div class="flex flex-col gap-3 w-full" style="flex: 4;">
										<button
											type="button"
											onclick={() => (qrLink = station.link)}
											class="w-full bg-primary hover:bg-on-primary-fixed-variant text-on-primary font-label-caps text-label-caps py-sm px-md rounded-lg transition-opacity flex items-center justify-center gap-xs"
										>
											<Icon name="qrCode2" size={18} />
											{m.emsp_generate_qr()}
										</button>
										<a
											href={station.link.url}
											target="_blank"
											rel="noopener"
											class="w-full border border-outline hover:bg-surface-container-low text-primary font-label-caps text-label-caps py-sm px-md rounded-lg transition-colors flex items-center justify-center gap-xs text-center"
										>
											<Icon name="openInNew" size={18} />
											{m.emsp_view_public()}
										</a>
									</div>
								</div>
							{/each}
						</div>
					</section>
				{/if}
			{/each}
		{/if}
	</main>

	{#if qrLink}
		<QrModal link={qrLink} onClose={() => (qrLink = null)} />
	{/if}
</div>
