// Mounting of the components in the tests, with Svelte 5's own API (no testing library).

import { flushSync, mount, unmount, type Component } from 'svelte';

const mounted: (() => void)[] = [];

/**
 * Mounts a component into a new element of document.body and runs its first effects, like the page's main.ts does
 * in the browser. Returns the element. Call cleanup() after each test.
 */
export function render<Props extends Record<string, unknown>>(component: Component<Props>, props: Props): HTMLElement {
	const target = document.body.appendChild(document.createElement('div'));
	const instance = mount(component as Component<Record<string, unknown>>, { target, props });
	flushSync();
	mounted.push(() => unmount(instance));
	return target;
}

/** Unmounts every component rendered by the test (stopping its timers and requests) and empties the document. */
export function cleanup(): void {
	for (const destroy of mounted.splice(0)) {
		destroy();
	}
	document.body.replaceChildren();
}

/** The text of an element with its whitespace collapsed (template indentation removed). */
export function textOf(element: Element | null | undefined): string {
	return (element?.textContent ?? '').replace(/\s+/g, ' ').trim();
}
