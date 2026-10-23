import { BookingError } from "./errors.ts";

export const MINUTE_MS = 60_000;
export const DAY_MS = 24 * 60 * MINUTE_MS;

const ISO_INSTANT = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}(:\d{2}(\.\d+)?)?(Z|[+-]\d{2}:\d{2})$/;
const ISO_DAY = /^\d{4}-\d{2}-\d{2}$/;

/** Parse an ISO-8601 instant that carries an explicit offset ("Z" or "+02:00") into epoch ms. */
export function parseInstant(text: string): number {
	if (!ISO_INSTANT.test(text)) {
		throw new BookingError("INVALID_TIME", `not an ISO instant with an offset: ${text}`);
	}
	const ms = Date.parse(text);
	if (Number.isNaN(ms)) throw new BookingError("INVALID_TIME", `invalid instant: ${text}`);
	return ms;
}

/** Validate a calendar day written YYYY-MM-DD. */
export function parseDay(text: string): string {
	if (!ISO_DAY.test(text) || Number.isNaN(Date.parse(`${text}T00:00:00Z`))) {
		throw new BookingError("INVALID_TIME", `not a day (YYYY-MM-DD): ${text}`);
	}
	return text;
}

function pad(n: number): string {
	return String(n).padStart(2, "0");
}

/** The calendar day (YYYY-MM-DD) an instant falls on. */
export function dayKey(ms: number): string {
	const d = new Date(ms);
	return `${d.getUTCFullYear()}-${pad(d.getUTCMonth() + 1)}-${pad(d.getUTCDate())}`;
}

export function formatInstant(ms: number): string {
	return new Date(ms).toISOString().replace(".000Z", "Z");
}
