// Stand-in for $lib/bootstrap in the page tests. The real module parses the Go page shell's JSON once and caches it
// for the life of the page, so a test file could only ever render one view model. With
//
//     vi.mock('$lib/bootstrap', () => import('../../test/bootstrap'));
//
// every test sets its own with setPageData(). The data goes through JSON like the Go shell's, so the component gets
// exactly what JSON.parse would give it.

import type { PageBootstrap } from '$lib/contract.gen';

let bootstrap: PageBootstrap = { page: null };

/** Sets the view model that the next page rendered reads (PageBootstrap.page). */
export function setPageData(page: unknown): void {
	bootstrap = JSON.parse(JSON.stringify({ page })) as PageBootstrap;
}

export function getBootstrap(): PageBootstrap {
	return bootstrap;
}

export function getPageData<T>(): T | null {
	return (bootstrap.page ?? null) as T | null;
}
