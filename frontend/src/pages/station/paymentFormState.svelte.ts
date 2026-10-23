// State of the payment link form. It lives at page level, so the values survive the refreshes that hide and show
// the form, and the sent state keeps the form from coming back until the connector leaves "Preparing".

import type { ChargeRequest } from '$lib/contract.gen';

export class PaymentFormState {
	email = $state('');
	needsInvoice = $state(false);
	fiscalId = $state('');
	name = $state('');
	address = $state('');
	invoiceEmail = $state('');
	sent = $state(false);

	/** The request body for a connector. */
	request(poolCode: number, stationName: string, connectorId: number): ChargeRequest {
		const optional = (value: string) => (this.needsInvoice && value.trim() ? value.trim() : null);
		return {
			pool_code: poolCode,
			station_name: stationName,
			connector_id: connectorId,
			email: this.email.trim(),
			requires_invoice: this.needsInvoice,
			receipt_fiscal_id: optional(this.fiscalId),
			receipt_name: optional(this.name),
			receipt_address: optional(this.address),
			receipt_email: optional(this.invoiceEmail)
		};
	}

	/** Clears the values once the request was sent, so they do not linger for the next driver. */
	markSent(): void {
		this.email = this.fiscalId = this.name = this.address = this.invoiceEmail = '';
		this.needsInvoice = false;
		this.sent = true;
	}
}
