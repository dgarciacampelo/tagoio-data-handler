// Presentation of the OCPP connector statuses: the label and the color of each one. The Record is exhaustive, so a
// new status in Go (internal/dto) fails the type check until it is mapped here.

import { m } from '$lib/paraglide/messages';
import type { ConnectorStatus } from './contract.gen';

export interface StatusStyle {
	label: () => string;
	background: string; // Tailwind background class (status token)
}

const styles: Record<ConnectorStatus, StatusStyle> = {
	Available: { label: m.status_available, background: 'bg-status-available' },
	Preparing: { label: m.status_preparing, background: 'bg-status-preparing' },
	Charging: { label: m.status_charging, background: 'bg-status-charging' },
	SuspendedEV: { label: m.status_suspended, background: 'bg-status-suspended' },
	SuspendedEVSE: { label: m.status_suspended, background: 'bg-status-suspended' },
	Finishing: { label: m.status_finishing, background: 'bg-status-finishing' },
	Faulted: { label: m.status_error, background: 'bg-status-error' },
	Reserved: { label: m.status_unavailable, background: 'bg-status-unavailable' },
	Unavailable: { label: m.status_unavailable, background: 'bg-status-unavailable' }
};

export function statusStyle(status: ConnectorStatus): StatusStyle {
	return styles[status] ?? styles.Unavailable;
}
