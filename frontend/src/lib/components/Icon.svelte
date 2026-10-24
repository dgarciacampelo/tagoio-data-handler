<!--
@component
Inline SVG icon from the vendored Material Symbols set ($lib/icons.ts). Decorative by default (aria-hidden); pass
`label` for icons that carry meaning on their own. `size` is in pixels (the former icon font's font-size).

Usage: <Icon name="bolt" size={20} class="text-primary" />
-->
<script lang="ts">
	import type { SVGAttributes } from 'svelte/elements';
	import { icons, type IconName } from '$lib/icons';

	interface Props extends Omit<SVGAttributes<SVGSVGElement>, 'viewBox' | 'width' | 'height'> {
		name: IconName;
		size?: number;
		label?: string;
	}

	let { name, size = 24, label, class: className = '', ...rest }: Props = $props();

	const icon = $derived(icons[name]);
</script>

<svg
	xmlns="http://www.w3.org/2000/svg"
	viewBox={icon.viewBox}
	width={size}
	height={size}
	class="inline-block shrink-0 {className}"
	role={label ? 'img' : undefined}
	aria-label={label}
	aria-hidden={label ? undefined : true}
	{...rest}
>
	<!-- Trusted, static markup from $lib/icons.ts (never user data) -->
	{@html icon.body}
</svg>
