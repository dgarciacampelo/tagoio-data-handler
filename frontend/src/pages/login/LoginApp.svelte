<!--
@component
Admin login (eMSP dashboard and audits). The server checks the credentials and sets the session cookie, then the
page goes back to the page that asked for the login.
-->
<script lang="ts">
	import { apiFetch } from '$lib/api';
	import { getPageData } from '$lib/bootstrap';
	import type { LoginRequest, LoginResult, LoginView } from '$lib/contract.gen';
	import Icon from '$lib/components/Icon.svelte';
	import { errorMessage } from '$lib/errors';
	import { m } from '$lib/paraglide/messages';

	const view = getPageData<LoginView>();

	let user = $state('');
	let password = $state('');
	let submitting = $state(false);
	let error = $state('');

	async function submit(event: SubmitEvent) {
		event.preventDefault();
		if (submitting) return;
		submitting = true;
		error = '';
		try {
			const body: LoginRequest = { user, password, redirect: view?.redirect ?? '' };
			const result = await apiFetch<LoginResult>('POST', '/api/login', body);
			window.location.assign(result.redirect);
		} catch (failure) {
			error = errorMessage(failure);
			submitting = false;
		}
	}

	const inputClass =
		'w-full bg-surface-container-lowest border border-outline-variant rounded-lg px-sm py-sm font-body-md text-on-surface focus:outline-none focus:border-primary focus:ring-1 focus:ring-primary';
</script>

<div class="bg-background text-on-background min-h-screen pt-16 pb-10 font-body-md text-body-md antialiased">
	<header class="fixed top-0 left-0 w-full z-40 flex justify-center items-center px-md h-16 bg-surface border-b border-outline-variant shadow-sm">
		<div class="text-headline-md font-headline-md font-bold text-primary flex items-center gap-2">
			<Icon name="bolt" />
			{m.app_brand()}
		</div>
	</header>

	<!-- ! max-w-[28rem], not max-w-md: the theme's spacing names (md = 16px) win over the container sizes -->
	<main class="max-w-[28rem] mx-auto p-4 md:p-8 w-full">
		<section class="bg-surface-container-lowest border border-outline-variant rounded-xl shadow-sm p-lg mt-xl">
			<h1 class="font-headline-md text-headline-md text-on-surface mb-md flex items-center gap-2">
				<Icon name="lock" class="text-primary" />
				{m.login_title()}
			</h1>

			<form class="space-y-md" onsubmit={submit}>
				<div class="flex flex-col gap-base">
					<label class="font-label-sm text-label-sm text-on-surface" for="user">{m.login_user()}</label>
					<input class={inputClass} id="user" type="text" autocomplete="username" required bind:value={user} />
				</div>
				<div class="flex flex-col gap-base">
					<label class="font-label-sm text-label-sm text-on-surface" for="password">{m.login_password()}</label>
					<input class={inputClass} id="password" type="password" autocomplete="current-password" required bind:value={password} />
				</div>

				{#if error}
					<div class="text-error text-label-sm bg-error-container p-sm rounded-lg" role="alert">{error}</div>
				{/if}

				<button
					type="submit"
					disabled={submitting}
					class="w-full bg-primary hover:bg-on-primary-fixed-variant text-on-primary font-label-caps text-label-caps py-sm px-md rounded-lg shadow-sm transition-opacity flex items-center justify-center gap-xs disabled:opacity-50 disabled:cursor-not-allowed"
				>
					<Icon name={submitting ? 'hourglassEmpty' : 'login'} size={18} class={submitting ? 'animate-spin' : ''} />
					{submitting ? m.login_processing() : m.login_submit()}
				</button>
			</form>
		</section>
	</main>
</div>
