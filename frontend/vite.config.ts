// Vite configuration of the Svelte pages (Multi-Page Application).
//
// The Go server owns routing and renders the HTML shell of every page (internal/webui), so Vite only builds the
// JavaScript/CSS entry points:
//   - Development: Go injects <script> tags pointing at this dev server (HMR).
//   - Production:  Go reads .vite/manifest.json from the build output and serves the hashed bundles under /assets/
//                  (embedded in the binary with go:embed).
//
// Every page is registered automatically: src/pages/<page>/main.ts is the entry "<page>" (keep in sync with
// internal/webui entrySource).

import { existsSync, readdirSync } from 'node:fs';
import { resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { defineConfig } from 'vite';
import { svelte } from '@sveltejs/vite-plugin-svelte';
import tailwindcss from '@tailwindcss/vite';
import { paraglideVitePlugin } from '@inlang/paraglide-js';

const root = fileURLToPath(new URL('.', import.meta.url));
const devServerPort = 5173;

const pagesDir = resolve(root, 'src/pages');
const pageEntries = Object.fromEntries(
	(existsSync(pagesDir) ? readdirSync(pagesDir, { withFileTypes: true }) : [])
		.filter((entry) => entry.isDirectory())
		.map((entry) => [entry.name, resolve(pagesDir, entry.name, 'main.ts')])
);

export default defineConfig({
	plugins: [
		paraglideVitePlugin({
			project: './project.inlang',
			outdir: './src/lib/paraglide',
			strategy: ['baseLocale'] // Spanish only, for now
		}),
		tailwindcss(),
		svelte()
	],
	resolve: {
		alias: { $lib: resolve(root, 'src/lib') }
	},
	build: {
		// Embedded into the Go binary by internal/webui (go:embed all:dist, build tag "embedui")
		outDir: resolve(root, '../internal/webui/dist'),
		emptyOutDir: true,
		manifest: true,
		rolldownOptions: {
			input: pageEntries,
			// Fail instead of shipping a broken bundle: an undefined import means a missing export, e.g. Paraglide
			// messages that were not compiled.
			onwarn(warning, defaultHandler) {
				if (warning.code === 'IMPORT_IS_UNDEFINED') {
					throw new Error(`Build aborted: ${warning.message}`);
				}
				defaultHandler(warning);
			}
		}
	},
	server: {
		port: devServerPort,
		strictPort: true,
		// Pages are served by Go while scripts and assets come from here.
		origin: `http://localhost:${devServerPort}`,
		cors: { origin: /^https?:\/\/(localhost|127\.0\.0\.1)(:\d+)?$/ }
	}
});
