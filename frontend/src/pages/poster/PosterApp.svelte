<!--
@component
Printable QR poster of a station (A5 or A4, portrait): "scan to charge", the QR code in the brand green frame, and
the operator, pool, station and URL. It opens the print dialog once the QR image is loaded.
-->
<script lang="ts">
	import { getPageData } from '$lib/bootstrap';
	import type { PosterPage } from '$lib/contract.gen';
	import { m } from '$lib/paraglide/messages';

	const view = getPageData<PosterPage>()!;
	const a4 = view.size === 'a4';

	function printWhenLoaded() {
		// Let the browser lay the page out before the print dialog freezes it.
		requestAnimationFrame(() => window.print());
	}
</script>

<svelte:head>
	{@html `<style>@page { size: ${a4 ? 'A4' : 'A5'} portrait; margin: 0; }</style>`}
</svelte:head>

<div class="poster" class:a4>
	<div class="subtitle">{m.poster_subtitle()}</div>

	<div class="qr-container">
		<img class="qr-image" src={view.link.qr_image_url} alt={m.poster_qr_alt()} onload={printWhenLoaded} />
	</div>

	<div class="info-box">
		<p>{m.poster_cpo()} <span class="bold">{view.link.cpo_name}</span></p>
		<p>{m.poster_pool()} <span class="bold">{view.link.pool_code}</span></p>
		<p>{m.poster_station()} <span class="bold">{view.link.station_name}</span></p>
		<p>{m.poster_url()} <span class="bold">{view.link.url}</span></p>
	</div>
</div>

<style>
	/* Plain CSS, as the former print templates: the poster must print the same everywhere. */
	:global(html),
	:global(body) {
		background-color: white;
	}

	.poster {
		margin: 0;
		padding: 0;
		display: flex;
		flex-direction: column;
		align-items: center;
		justify-content: center;
		height: 100vh;
		font-family: 'Segoe UI', Tahoma, Geneva, Verdana, sans-serif;
		background-color: white;
		color: #1a1a1a;
		-webkit-print-color-adjust: exact;
		print-color-adjust: exact;
	}

	.subtitle {
		font-size: 1.5rem;
		color: #4a4a4a;
		margin-top: 0;
		margin-bottom: 25px;
	}

	.qr-container {
		padding: 15px;
		border: 3px solid #00571b;
		border-radius: 16px;
		display: inline-block;
	}

	.qr-image {
		display: block;
		width: 240px;
		height: auto;
		image-rendering: pixelated;
	}

	.info-box {
		margin-top: 25px;
		text-align: center;
		font-size: 1.2rem;
		color: #4a4a4a;
		line-height: 1.4;
		overflow-wrap: break-word;
		max-width: 90%;
	}

	.info-box p {
		margin: 8px 0;
	}

	.bold {
		font-weight: bold;
		color: #1a1a1a;
	}

	/* A4: larger type and code */
	.a4 .subtitle {
		font-size: 2.2rem;
		margin-bottom: 40px;
	}

	.a4 .qr-container {
		padding: 20px;
		border-width: 4px;
		border-radius: 24px;
	}

	.a4 .qr-image {
		width: 350px;
	}

	.a4 .info-box {
		margin-top: 40px;
		font-size: 1.6rem;
	}
</style>
