// Display formatting of numbers, for Spanish readers (decimal comma).

const formatters = new Map<string, Intl.NumberFormat>();

/** Formats a number with a fixed number of decimals ("12,34"). */
export function formatFixed(value: number, decimals: number): string {
	const key = `fixed-${decimals}`;
	let formatter = formatters.get(key);
	if (!formatter) {
		formatter = new Intl.NumberFormat('es-ES', { minimumFractionDigits: decimals, maximumFractionDigits: decimals });
		formatters.set(key, formatter);
	}
	return formatter.format(value);
}

/** Formats a number with up to the given number of decimals, without trailing zeros ("0,1534"). */
export function formatUpTo(value: number, decimals: number): string {
	const key = `upto-${decimals}`;
	let formatter = formatters.get(key);
	if (!formatter) {
		formatter = new Intl.NumberFormat('es-ES', { maximumFractionDigits: decimals });
		formatters.set(key, formatter);
	}
	return formatter.format(value);
}
