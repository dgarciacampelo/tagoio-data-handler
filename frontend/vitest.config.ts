// Vitest configuration: the Vite configuration (Svelte, Paraglide, $lib alias) plus a DOM for the component tests.
//
// Component tests mount the real components with Svelte 5's own API (mount/flushSync/unmount) in happy-dom; the
// helpers and the known-bug convention are in src/test/.

import { defineConfig, mergeConfig } from 'vitest/config';
import viteConfig from './vite.config.ts';

export default mergeConfig(
	viteConfig,
	defineConfig({
		// Svelte's client runtime: without the "browser" condition Vitest resolves svelte to its server build, where
		// mount() is not available.
		resolve: { conditions: ['browser'] },
		test: {
			environment: 'happy-dom',
			include: ['src/**/*.test.ts'],
			// Every test starts from clean mocks and the real globals (stubFetch's fetch is undone after each test).
			restoreMocks: true,
			unstubGlobals: true
		}
	})
);
