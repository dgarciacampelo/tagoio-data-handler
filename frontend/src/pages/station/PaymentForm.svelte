<!--
@component
Payment link form, shown while a vehicle is plugged in: the driver's email (and invoice details, optionally). The
server sends the CSMS the request; the driver gets the virtual POS link by email.
-->
<script lang="ts">
	import { apiFetch } from '$lib/api';
	import type { ChargeRequestResult } from '$lib/contract.gen';
	import Icon from '$lib/components/Icon.svelte';
	import { m } from '$lib/paraglide/messages';
	import type { PaymentFormState } from './paymentFormState.svelte';

	interface Props {
		form: PaymentFormState;
		poolCode: number;
		stationName: string;
		connectorId: number;
		onShowTerms: () => void;
	}

	let { form, poolCode, stationName, connectorId, onShowTerms }: Props = $props();

	let formElement = $state<HTMLFormElement>();
	let valid = $state(false);
	let submitting = $state(false);
	let failed = $state(false);

	const inputClass =
		'w-full bg-surface-container-lowest border border-outline-variant rounded-lg px-sm py-sm font-body-md text-on-surface focus:outline-none focus:border-primary focus:ring-1 focus:ring-primary';

	// The browser's own validation (email format, required fields) enables the button.
	$effect(() => {
		void form.needsInvoice; // The required fields change with the checkbox
		queueMicrotask(revalidate);
	});

	function revalidate() {
		valid = formElement?.checkValidity() ?? false;
	}

	async function submit(event: SubmitEvent) {
		event.preventDefault();
		if (submitting || !valid) return;
		submitting = true;
		failed = false;
		try {
			await apiFetch<ChargeRequestResult>('POST', '/api/charge-request', form.request(poolCode, stationName, connectorId));
			form.markSent();
		} catch {
			failed = true;
		} finally {
			submitting = false;
		}
	}
</script>

<div class="bg-surface-container-lowest border border-outline-variant rounded-xl shadow-sm p-lg h-full">
	<h3 class="font-headline-md text-headline-md text-on-surface mb-md">{m.station_payment_title()}</h3>

	{#if form.sent}
		<div class="flex flex-col items-center text-center gap-sm py-lg">
			<Icon name="checkCircle" size={48} class="text-secondary" />
			<h4 class="font-headline-md text-on-surface">{m.station_payment_sent_title()}</h4>
			<p class="font-body-md text-on-surface-variant">{m.station_payment_sent_text()}</p>
		</div>
	{:else}
		<form class="space-y-md" bind:this={formElement} oninput={revalidate} onsubmit={submit} novalidate>
			<div class="flex flex-col gap-base">
				<label class="font-label-sm text-label-sm text-on-surface" for="email">
					{m.station_payment_email()} <span class="text-error">*</span>
				</label>
				<input
					class={inputClass}
					id="email"
					type="email"
					autocomplete="email"
					required
					placeholder={m.station_payment_email_placeholder()}
					bind:value={form.email}
				/>
			</div>

			<div class="flex items-center gap-sm mt-sm border-t border-outline-variant pt-md">
				<input
					class="w-4 h-4 rounded border-outline-variant text-primary accent-primary focus:ring-primary focus:ring-offset-0 bg-surface-container-lowest"
					id="need-receipt"
					type="checkbox"
					bind:checked={form.needsInvoice}
				/>
				<label class="font-label-sm text-label-sm text-on-surface cursor-pointer select-none" for="need-receipt">
					{m.station_payment_invoice()}
				</label>
			</div>

			{#if form.needsInvoice}
				<div class="flex flex-col gap-md mt-sm">
					<div class="flex flex-col gap-base">
						<label class="font-label-sm text-label-sm text-on-surface" for="receipt_fiscal_id">
							{m.station_payment_fiscal_id()} <span class="text-error">*</span>
						</label>
						<input class={inputClass} id="receipt_fiscal_id" type="text" required placeholder={m.station_payment_fiscal_id_placeholder()} bind:value={form.fiscalId} />
					</div>
					<div class="flex flex-col gap-base">
						<label class="font-label-sm text-label-sm text-on-surface" for="receipt_name">
							{m.station_payment_name()} <span class="text-error">*</span>
						</label>
						<input class={inputClass} id="receipt_name" type="text" required autocomplete="organization" placeholder={m.station_payment_name_placeholder()} bind:value={form.name} />
					</div>
					<div class="flex flex-col gap-base">
						<label class="font-label-sm text-label-sm text-on-surface" for="receipt_address">
							{m.station_payment_address()} <span class="text-error">*</span>
						</label>
						<input class={inputClass} id="receipt_address" type="text" required autocomplete="street-address" placeholder={m.station_payment_address_placeholder()} bind:value={form.address} />
					</div>
					<div class="flex flex-col gap-base">
						<label class="font-label-sm text-label-sm text-on-surface" for="receipt_email">
							{m.station_payment_invoice_email()} <span class="text-error">*</span>
						</label>
						<input class={inputClass} id="receipt_email" type="email" required placeholder={m.station_payment_invoice_email_placeholder()} bind:value={form.invoiceEmail} />
					</div>
				</div>
			{/if}

			{#if failed}
				<div class="text-error text-label-sm bg-error-container p-sm rounded-lg mt-sm" role="alert">
					{m.station_payment_error()}
				</div>
			{/if}

			<div class="mt-sm text-center">
				<p class="font-label-sm text-on-surface-variant">
					{m.station_payment_terms_prefix()}
					<button type="button" onclick={onShowTerms} class="text-primary underline hover:opacity-80 transition-opacity">
						{m.station_payment_terms_link()}
					</button>
				</p>
			</div>

			<div class="pt-sm">
				<button
					type="submit"
					disabled={!valid || submitting}
					class="w-full bg-primary hover:bg-on-primary-fixed-variant text-on-primary font-label-caps text-label-caps py-sm px-md rounded-lg shadow-sm transition-opacity flex items-center justify-center gap-xs disabled:opacity-50 disabled:cursor-not-allowed"
				>
					<Icon name={submitting ? 'hourglassEmpty' : 'payments'} size={18} class={submitting ? 'animate-spin' : ''} />
					<span>{submitting ? m.station_payment_processing() : m.station_payment_submit()}</span>
				</button>
			</div>
		</form>
	{/if}
</div>
