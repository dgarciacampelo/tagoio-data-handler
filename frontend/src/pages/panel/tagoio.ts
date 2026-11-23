// Bridge with the TagoIO dashboard that embeds the panel (a Custom Widget). TagoIO gives the page two things:
//   - the token of the person who has the dashboard open (onSyncUserInformation), which the server checks;
//   - the data of the widget's variables (onRealtime), whose records carry the ID of the installation's device
//     selected in the dashboard. The widget's own settings only name the blueprint placeholder, not the device.
//
// The token is kept in memory only and sent to this service alone.

const sdkURL = 'https://admin.tago.io/dist/custom-widget.min.js';
const deviceIDPattern = /^[0-9a-f]{24}$/;

/** What the dashboard tells the panel. */
export interface TagoContext {
	token: string;
	deviceId: string;
}

interface RealtimeRecord {
	device?: string;
	origin?: string;
}

/** The part of TagoIO's Custom Widget SDK the panel uses. */
export interface TagoSDK {
	onRealtime(callback: (data: { result?: RealtimeRecord[] }[]) => void): void;
	onSyncUserInformation(callback: (info: { token: string | null }) => void): void;
	ready(options?: { header?: { absolute?: boolean } }): void;
}

declare global {
	interface Window {
		TagoIO?: TagoSDK;
	}
}

/** Why the panel can't learn its context from TagoIO. */
export type TagoProblem = 'not_embedded' | 'no_data';

export class TagoError extends Error {
	constructor(readonly problem: TagoProblem) {
		super(problem);
		this.name = 'TagoError';
	}
}

/** Returns the device ID carried by the first realtime record that has one. */
export function deviceOf(data: { result?: RealtimeRecord[] }[]): string | undefined {
	for (const group of data ?? []) {
		for (const record of group.result ?? []) {
			for (const id of [record.device, record.origin]) {
				if (id && deviceIDPattern.test(id)) return id;
			}
		}
	}
	return undefined;
}

/**
 * Waits for the token and the device from TagoIO. When the dashboard later sends records of another device (the
 * CPO picked another installation), onDeviceChange is called.
 */
export function connectToTago(
	sdk: TagoSDK,
	onDeviceChange: (deviceId: string) => void,
	timeoutMs = 20000
): Promise<TagoContext> {
	return new Promise((resolve, reject) => {
		let token: string | undefined;
		let deviceId: string | undefined;
		let settled = false;
		const timer = setTimeout(() => {
			if (!settled) {
				settled = true;
				reject(new TagoError('no_data'));
			}
		}, timeoutMs);
		const check = () => {
			if (!settled && token && deviceId) {
				settled = true;
				clearTimeout(timer);
				resolve({ token, deviceId });
			}
		};

		sdk.onSyncUserInformation((info) => {
			token = info?.token ?? undefined;
			check();
		});
		sdk.onRealtime((data) => {
			const id = deviceOf(data);
			if (!id) return;
			if (settled && deviceId && id !== deviceId) {
				onDeviceChange(id);
				return;
			}
			deviceId = id;
			check();
		});
		sdk.ready({ header: { absolute: true } });
	});
}

/** Loads TagoIO's SDK, which only works inside a TagoIO dashboard. */
export async function loadTagoSDK(): Promise<TagoSDK> {
	if (window.parent === window) {
		throw new TagoError('not_embedded');
	}
	if (!window.TagoIO) {
		await new Promise<void>((resolve, reject) => {
			const script = document.createElement('script');
			script.src = sdkURL;
			script.onload = () => resolve();
			script.onerror = () => reject(new TagoError('no_data'));
			document.head.appendChild(script);
		});
	}
	if (!window.TagoIO) {
		throw new TagoError('no_data');
	}
	return window.TagoIO;
}
