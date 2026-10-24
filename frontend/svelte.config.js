import { vitePreprocess } from '@sveltejs/vite-plugin-svelte';

/** @type {import('@sveltejs/vite-plugin-svelte').SvelteConfig} */
export default {
	preprocess: vitePreprocess(),
	vitePlugin: {
		// Enforce Svelte 5 runes for our own components (no legacy `export let` / `$:` syntax), while leaving
		// third-party libraries in node_modules to their own mode.
		dynamicCompileOptions({ filename }) {
			if (!filename.includes('node_modules')) {
				return { runes: true };
			}
		}
	}
};
