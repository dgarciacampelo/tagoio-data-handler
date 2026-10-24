// Data rendered by the Go page shell (internal/webui) into <script id="bootstrap" type="application/json">. The types
// are generated from Go (internal/dto).

import type { PageBootstrap } from './contract.gen';

let cached: PageBootstrap | undefined;

/** Returns the bootstrap data of the current page (parsed once). */
export function getBootstrap(): PageBootstrap {
	if (!cached) {
		const element = document.getElementById('bootstrap');
		if (!element?.textContent) {
			throw new Error('Missing #bootstrap data: the pages must be served by the Go page shell');
		}
		cached = JSON.parse(element.textContent) as PageBootstrap;
	}
	return cached;
}

/** Returns the page's view model built by Go (PageBootstrap.page), or null when Go could not build it. */
export function getPageData<T>(): T | null {
	return (getBootstrap().page ?? null) as T | null;
}
