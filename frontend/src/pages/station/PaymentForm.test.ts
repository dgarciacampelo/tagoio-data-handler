import { flushSync } from 'svelte';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { ChargeRequestResult } from '$lib/contract.gen';
import { m } from '$lib/paraglide/messages';
import { jsonBody, stubFetch } from '../../test/fetch';
import { cleanup, render, textOf } from '../../test/render';
import PaymentForm from './PaymentForm.svelte';
import { PaymentFormState } from './paymentFormState.svelte';

const answer: ChargeRequestResult = { status: 'pending_authorization', amount_requested: 50 };

/** Renders the form of connector 1 of station EVC01 (pool 12) and types a valid email. */
function renderFilledForm() {
	const form = new PaymentFormState();
	const target = render(PaymentForm, { form, poolCode: 12, stationName: 'EVC01', connectorId: 1, onShowTerms: () => {} });
	const email = target.querySelector<HTMLInputElement>('#email')!;
	email.value = 'driver@example.com';
	email.dispatchEvent(new Event('input', { bubbles: true }));
	flushSync();
	return { form, target, formElement: target.querySelector('form')! };
}

function submit(formElement: HTMLFormElement): void {
	formElement.dispatchEvent(new SubmitEvent('submit', { bubbles: true, cancelable: true }));
	flushSync();
}

beforeEach(() => {
	vi.useFakeTimers();
});

afterEach(() => {
	cleanup();
	vi.useRealTimers();
});

describe('PaymentForm', () => {
	it('sends the charge request once, even when submitted again while it is in flight', async () => {
		const fetch = stubFetch(() => ({ body: answer, delay: 2000 }));
		const { form, target, formElement } = renderFilledForm();
		const submitButton = target.querySelector<HTMLButtonElement>('button[type="submit"]')!;
		expect(submitButton.disabled, 'submit button disabled with a valid email').toBe(false);

		submit(formElement);
		expect(textOf(submitButton)).toBe(m.station_payment_processing());
		expect(submitButton.disabled).toBe(true);
		submit(formElement); // Submitted again before the answer arrives
		await vi.advanceTimersByTimeAsync(2000);
		flushSync();

		expect(fetch).toHaveBeenCalledOnce();
		const [url, init] = fetch.mock.calls[0];
		expect(url).toBe('/api/charge-request');
		expect(init?.method).toBe('POST');
		expect(jsonBody(init)).toEqual({
			pool_code: 12,
			station_name: 'EVC01',
			connector_id: 1,
			email: 'driver@example.com',
			requires_invoice: false,
			receipt_fiscal_id: null,
			receipt_name: null,
			receipt_address: null,
			receipt_email: null
		});
		expect(form.sent).toBe(true);
		expect(textOf(target)).toContain(m.station_payment_sent_title());
	});
});
