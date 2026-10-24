// Entry helper shared by every page: global styles and mounting into the Go page shell.

import '@fontsource-variable/inter/index.css';
import '../app.css';
import { mount, type Component } from 'svelte';

/** Mounts a page's root component into <div id="app"> rendered by the Go page shell. */
export function mountPage(App: Component): void {
	const target = document.getElementById('app');
	if (!target) {
		throw new Error('Missing #app element: the pages must be served by the Go page shell');
	}
	mount(App, { target });
}
