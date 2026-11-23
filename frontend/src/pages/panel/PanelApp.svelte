<!--
@component
Management panel of a pool, embedded in the CPO dashboard of TagoIO (a Custom Widget per dashboard tab). TagoIO gives
it the token of the person who has the dashboard open and the selected installation; the server checks that the
person manages that installation (its group_id) and starts a panel session.

Each TagoIO tab opens one section with the URL hash (#general, #tarifas, #cpo, #en-curso); without a hash the
panel shows its own tabs. The live state (stations, sessions) comes from one stream, shared by the sections.
-->
<script lang="ts">
	import { errorMessage } from '$lib/errors';
	import type { PanelLive, PanelOverview, PanelSession } from '$lib/contract.gen';
	import Icon from '$lib/components/Icon.svelte';
	import { m } from '$lib/paraglide/messages';
	import { PanelClient } from './client';
	import { connectToTago, loadTagoSDK, TagoError, type TagoSDK } from './tagoio';
	import { sectionOf, sections, type Section } from './panel';
	import GeneralTab from './GeneralTab.svelte';
	import TariffsCardsTab from './TariffsCardsTab.svelte';
	import CpoTab from './CpoTab.svelte';
	import LiveSessionsTab from './LiveSessionsTab.svelte';

	interface Props {
		/** The TagoIO SDK; loaded from TagoIO when not given (tests pass a fake). */
		sdk?: TagoSDK;
	}

	let { sdk }: Props = $props();

	const fixedSection = sectionOf(window.location.hash);
	let section = $state<Section>(fixedSection ?? 'general');

	let client = $state.raw<PanelClient>();
	let session = $state.raw<PanelSession>();
	let overview = $state.raw<PanelOverview>();
	let live = $state.raw<PanelLive>();
	let connected = $state(false);
	let failure = $state('');
	let toast = $state('');
	let toastTimer: ReturnType<typeof setTimeout> | undefined;

	function notify(message: string) {
		toast = message;
		clearTimeout(toastTimer);
		toastTimer = setTimeout(() => (toast = ''), 3500);
	}

	async function open() {
		failure = '';
		try {
			const tago = await connectToTago(sdk ?? (await loadTagoSDK()), () => window.location.reload());
			const next = new PanelClient(tago);
			session = await next.start();
			overview = await next.call<PanelOverview>('GET', '/overview');
			client = next;
		} catch (error) {
			if (error instanceof TagoError) {
				failure = error.problem === 'not_embedded' ? m.panel_not_embedded() : m.panel_no_tago_data();
			} else {
				failure = errorMessage(error);
			}
		}
	}

	$effect(() => {
		void open();
	});

	// The live stream, open while the panel has a client.
	$effect(() => {
		if (!client) return;
		return client.stream(
			(next) => (live = next),
			(ok) => (connected = ok)
		);
	});
</script>

<div class="bg-background text-on-background min-h-screen font-body-md text-body-md antialiased">
	{#if failure}
		<div class="max-w-[36rem] mx-auto p-8 text-center space-y-4" role="alert">
			<h1 class="text-headline-md font-headline-md">{m.panel_error_title()}</h1>
			<p class="text-on-surface-variant">{failure}</p>
			<button type="button" class="px-4 py-2 rounded-lg bg-primary text-on-primary font-medium" onclick={open}>
				{m.panel_retry()}
			</button>
		</div>
	{:else if !client || !overview || !session}
		<div class="flex items-center justify-center gap-3 p-12 text-on-surface-variant" role="status">
			<Icon name="progressActivity" class="animate-spin" />
			<span>{m.panel_connecting()}</span>
		</div>
	{:else}
		{#if !fixedSection}
			<nav class="flex gap-1 px-4 border-b border-outline-variant bg-surface overflow-x-auto" aria-label={m.panel_title()}>
				{#each sections as s (s.id)}
					<button
						type="button"
						class="px-4 py-3 text-sm whitespace-nowrap border-b-2 {section === s.id
							? 'border-primary text-primary font-semibold'
							: 'border-transparent text-on-surface-variant'}"
						aria-current={section === s.id ? 'page' : undefined}
						onclick={() => (section = s.id)}
					>
						{s.label()}
					</button>
				{/each}
			</nav>
		{/if}

		<main class="p-4 space-y-4">
			<p class="flex items-center gap-2 text-xs text-on-surface-variant">
				<span class="inline-block w-2 h-2 rounded-full {connected ? 'bg-secondary' : 'bg-outline'}"></span>
				{connected ? m.panel_live_on() : m.panel_live_reconnecting()} · {overview.installation_name}
			</p>

			{#if section === 'general'}
				<GeneralTab {client} {overview} {live} onoverview={(o) => (overview = o)} {notify} />
			{:else if section === 'tarifas'}
				<TariffsCardsTab {client} {overview} onoverview={(o) => (overview = o)} {notify} />
			{:else if section === 'cpo'}
				<CpoTab {client} {overview} onoverview={(o) => (overview = o)} {notify} />
			{:else}
				<LiveSessionsTab {overview} {live} />
			{/if}
		</main>
	{/if}

	{#if toast}
		<div class="fixed right-4 bottom-4 z-50 max-w-[22rem] rounded-lg bg-on-surface text-surface px-4 py-3 shadow-lg" role="status">
			{toast}
		</div>
	{/if}
</div>
