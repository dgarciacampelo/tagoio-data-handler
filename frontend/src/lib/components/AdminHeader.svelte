<!--
@component
Fixed header of the admin pages: the page title with its icon, an optional link back, and the logout button.
-->
<script lang="ts">
	import { apiFetch } from '$lib/api';
	import Icon from '$lib/components/Icon.svelte';
	import type { IconName } from '$lib/icons';
	import { m } from '$lib/paraglide/messages';

	interface Props {
		title: string;
		icon: IconName;
		back?: { href: string; label: string; icon: IconName };
	}

	let { title, icon, back }: Props = $props();

	async function logout() {
		await apiFetch('POST', '/api/logout').catch(() => undefined);
		window.location.assign('/login');
	}
</script>

<header
	class="fixed top-0 left-0 w-full z-40 flex {back ? 'justify-between' : 'justify-center'} items-center px-md h-16 bg-surface border-b border-outline-variant shadow-sm"
>
	<div class="text-headline-md font-headline-md font-bold text-primary flex items-center gap-2">
		<Icon name={icon} />
		<span class="hidden sm:inline">{title}</span>
	</div>
	<div class="flex items-center gap-md {back ? '' : 'absolute right-md'}">
		{#if back}
			<a href={back.href} class="text-on-surface-variant hover:text-primary transition-colors flex items-center gap-1 font-label-sm">
				<Icon name={back.icon} size={18} />
				{back.label}
			</a>
		{/if}
		<button
			type="button"
			onclick={logout}
			class="text-on-surface-variant hover:text-error transition-colors flex items-center gap-1 font-label-sm"
		>
			<Icon name="logout" size={18} />
			{m.common_logout()}
		</button>
	</div>
</header>
