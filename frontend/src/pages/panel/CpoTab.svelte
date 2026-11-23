<!--
@component
"Contacto CPO" section of the management panel: the operator details the drivers see, changed in place.
-->
<script lang="ts">
	import { errorMessage } from '$lib/errors';
	import type { PanelCPO, PanelOverview } from '$lib/contract.gen';
	import { m } from '$lib/paraglide/messages';
	import type { PanelClient } from './client';

	interface Props {
		client: PanelClient;
		overview: PanelOverview;
		onoverview: (overview: PanelOverview) => void;
		notify: (message: string) => void;
	}

	let { client, overview, onoverview, notify }: Props = $props();

	const fields: { key: keyof PanelCPO; label: () => string; type: string }[] = [
		{ key: 'name', label: m.panel_cpo_name, type: 'text' },
		{ key: 'fiscal_id', label: m.panel_cpo_fiscal_id, type: 'text' },
		{ key: 'address', label: m.panel_cpo_address, type: 'text' },
		{ key: 'phone', label: m.panel_cpo_phone, type: 'tel' },
		{ key: 'email', label: m.panel_cpo_email, type: 'email' },
		{ key: 'web', label: m.panel_cpo_web, type: 'url' }
	];

	let editing = $state(false);
	let saving = $state(false);
	let form = $state<PanelCPO>({ name: '', fiscal_id: '', address: '', phone: '', email: '', web: '' });

	async function save(event: SubmitEvent) {
		event.preventDefault();
		saving = true;
		try {
			onoverview(await client.call<PanelOverview>('PUT', '/cpo', form));
			notify(m.panel_cpo_saved());
			editing = false;
		} catch (error) {
			notify(errorMessage(error));
		} finally {
			saving = false;
		}
	}
</script>

<div class="grid gap-4 lg:grid-cols-[minmax(0,1.45fr)_minmax(0,1fr)] items-start">
	<section class="rounded-xl border border-outline-variant bg-surface-container-lowest p-4 space-y-3">
		<div class="flex items-center justify-between gap-2">
			<h2 class="text-title-md font-semibold">{m.panel_cpo_title()}</h2>
			{#if !editing}
				<button
					type="button"
					class="rounded border border-outline-variant px-3 py-1 text-sm"
					onclick={() => {
						form = { ...overview.cpo };
						editing = true;
					}}>{m.panel_edit()}</button
				>
			{/if}
		</div>
		{#if editing}
			<form class="space-y-3" onsubmit={save}>
				<div class="grid gap-3 sm:grid-cols-2">
					{#each fields as field (field.key)}
						<label class="grid gap-1 text-xs text-on-surface-variant">
							{field.label()}
							<input
								id="cpo-{field.key}"
								type={field.type}
								maxlength="200"
								required={field.key === 'name'}
								class="rounded border border-outline-variant bg-surface-container-lowest px-2 py-1.5 text-sm text-on-surface"
								bind:value={form[field.key]}
							/>
						</label>
					{/each}
				</div>
				<div class="flex justify-end gap-2">
					<button type="button" class="rounded border border-outline-variant px-3 py-1.5 text-sm" onclick={() => (editing = false)}>{m.panel_cancel()}</button>
					<button type="submit" class="rounded bg-primary text-on-primary px-3 py-1.5 text-sm font-medium disabled:opacity-50" disabled={saving}>
						{saving ? m.panel_saving() : m.panel_save()}
					</button>
				</div>
			</form>
		{:else}
			<dl class="grid grid-cols-[auto_1fr] gap-x-6 gap-y-2 text-sm">
				{#each fields as field (field.key)}
					<dt class="text-on-surface-variant">{field.label()}</dt>
					<dd class="break-words min-w-0">{overview.cpo[field.key] || '—'}</dd>
				{/each}
			</dl>
		{/if}
	</section>

	<section class="rounded-xl border border-outline-variant bg-surface-container-lowest p-4 space-y-2">
		<h2 class="text-title-md font-semibold">{m.panel_cpo_usage_title()}</h2>
		<p class="text-sm text-on-surface-variant">{m.panel_cpo_usage_text()}</p>
	</section>
</div>
