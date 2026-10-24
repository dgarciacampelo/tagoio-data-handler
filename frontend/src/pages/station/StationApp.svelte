<!--
@component
Public page of a station, opened by drivers from the station's QR code: the status of the selected connector (with
the payment form when a vehicle is plugged in, and the session metrics while charging), the operator and its
rates, the charging instructions and the legal terms. The live part refreshes every 5 seconds.
-->
<script lang="ts">
	import { getPageData } from '$lib/bootstrap';
	import { apiFetch } from '$lib/api';
	import type { StationLive, StationPage } from '$lib/contract.gen';
	import Icon from '$lib/components/Icon.svelte';
	import type { IconName } from '$lib/icons';
	import { m } from '$lib/paraglide/messages';
	import StatusCard from './StatusCard.svelte';
	import MetricsPanel from './MetricsPanel.svelte';
	import PaymentForm from './PaymentForm.svelte';
	import OperatorTab from './OperatorTab.svelte';
	import ChargingInstructions from './content/ChargingInstructions.svelte';
	import LegalTerms from './content/LegalTerms.svelte';
	import { PaymentFormState } from './paymentFormState.svelte';

	const view = getPageData<StationPage>()!;
	const pollInterval = 5000;

	let live = $state.raw<StationLive>(view.live);
	let tab = $state<'connectors' | 'rates' | 'info' | 'legal'>('connectors');
	const form = new PaymentFormState();

	const showSidePanel = $derived(live.config_loaded && live.panel !== 'none');

	// Refreshes the live part, paused while the page is hidden.
	$effect(() => {
		let controller: AbortController | undefined;
		const refresh = async () => {
			if (document.hidden) return;
			controller?.abort();
			controller = new AbortController();
			try {
				const next = await apiFetch<StationLive>('GET', view.poll_url, undefined, controller.signal);
				if (next.panel !== 'payment_form') form.sent = false; // The next driver gets the form again
				live = next;
			} catch {
				// Keep the last state; the next refresh retries
			}
		};
		const timer = setInterval(refresh, pollInterval);
		const onVisible = () => !document.hidden && refresh();
		document.addEventListener('visibilitychange', onVisible);
		return () => {
			clearInterval(timer);
			controller?.abort();
			document.removeEventListener('visibilitychange', onVisible);
		};
	});

	const tabs: { id: typeof tab; label: () => string; icon: IconName; activeIcon: IconName }[] = [
		{ id: 'connectors', label: m.station_tab_connectors, icon: 'evStation', activeIcon: 'evStationFill' },
		{ id: 'rates', label: m.station_tab_rates, icon: 'payments', activeIcon: 'paymentsFill' },
		{ id: 'info', label: m.station_tab_info, icon: 'info', activeIcon: 'infoFill' },
		{ id: 'legal', label: m.station_tab_legal, icon: 'gavel', activeIcon: 'gavelFill' }
	];
</script>

<div
	class="bg-background text-on-background min-h-screen pt-16 pb-20 font-body-md text-body-md antialiased selection:bg-primary-fixed selection:text-on-primary-fixed"
	style="min-height: max(884px, 100dvh);"
>
	<header
		class="fixed top-0 left-0 w-full z-50 flex justify-between items-center px-md h-16 bg-surface border-b border-outline-variant"
	>
		<div class="flex items-center gap-xs text-primary">
			<Icon name="bolt" />
		</div>
		<div class="text-headline-md font-headline-md font-bold text-primary text-center absolute left-1/2 -translate-x-1/2">
			{m.app_brand()}
		</div>
	</header>

	<main class="max-w-[1440px] mx-auto p-4 md:p-8 space-y-lg w-full">
		{#if tab === 'connectors'}
			<div
				class="grid grid-cols-1 md:grid-cols-12 gap-grid-gutter {showSidePanel && live.panel === 'payment_form' && form.needsInvoice && !form.sent
					? 'items-start'
					: 'items-stretch'}"
			>
				<div class="{showSidePanel ? 'md:col-span-8' : 'md:col-span-12'} flex flex-col gap-grid-gutter">
					{#if view.connectors.length > 0}
						<div class="flex gap-sm overflow-x-auto pb-1 mb-xs scrollbar-hide">
							{#each view.connectors as connector (connector.id)}
								<a
									href={connector.href}
									class="inline-flex items-center justify-center whitespace-nowrap px-lg py-sm rounded-lg font-label-sm transition-colors border {connector.selected
										? 'bg-primary text-on-primary border-primary shadow-sm font-bold'
										: 'bg-surface-container-lowest text-on-surface-variant border-outline-variant hover:bg-surface-container-low hover:text-primary'}"
								>
									{m.station_connector({ id: connector.id })}
								</a>
							{/each}
						</div>
					{/if}

					<div class="flex-1 flex flex-col">
						<StatusCard {live} poolCode={view.pool_code} stationName={view.station_name} connectorId={view.connector_id} />
					</div>

					{#if live.show_metrics}
						<MetricsPanel metrics={live.metrics} />
					{/if}
				</div>

				{#if showSidePanel}
					<div class="md:col-span-4 flex flex-col gap-grid-gutter">
						{#if live.panel === 'payment_form'}
							<PaymentForm
								{form}
								poolCode={view.pool_code}
								stationName={view.station_name}
								connectorId={view.connector_id}
								onShowTerms={() => (tab = 'legal')}
							/>
						{:else}
							<div
								class="bg-surface-container-lowest border border-outline-variant rounded-xl shadow-sm p-lg h-full flex flex-col items-center justify-center text-center gap-md py-xl"
							>
								<Icon name="evCharger" size={48} class="text-primary animate-pulse" />
								<h3 class="font-headline-md text-on-surface">{m.station_plug_in_title()}</h3>
								<p class="font-body-md text-on-surface-variant max-w-[280px]">
									{m.station_plug_in_text()}
									<strong>{m.station_connector({ id: view.connector_id })}</strong>.
								</p>
							</div>
						{/if}
					</div>
				{/if}
			</div>
		{:else if tab === 'rates'}
			<OperatorTab operator={live.operator} rates={live.rates} />
		{:else if tab === 'info'}
			<ChargingInstructions />
		{:else}
			<LegalTerms />
		{/if}
	</main>

	<nav
		class="fixed bottom-0 left-0 w-full z-50 flex justify-around items-center h-16 pb-safe px-xs border-t border-outline-variant bg-surface shadow-lg"
	>
		{#each tabs as item (item.id)}
			{@const active = tab === item.id}
			<button
				type="button"
				aria-current={active ? 'page' : undefined}
				onclick={() => {
					tab = item.id;
					window.scrollTo({ top: 0 });
				}}
				class="flex flex-col items-center justify-center w-full h-full {active
					? 'text-primary font-bold scale-95 transition-transform duration-150'
					: 'text-on-surface-variant hover:text-primary hover:bg-surface-container-low transition-colors'}"
			>
				<Icon name={active ? item.activeIcon : item.icon} />
				<span class="font-label-sm mt-1 text-center leading-tight">{item.label()}</span>
			</button>
		{/each}
	</nav>
</div>
