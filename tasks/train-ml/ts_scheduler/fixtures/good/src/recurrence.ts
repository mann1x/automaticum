import { BookingError } from "./errors.ts";
import { DAY_MS, MINUTE_MS, dayKey, parseDay, parseInstant } from "./time.ts";

export type WeeklyRule = {
	/** First occurrence, ISO instant with offset. */
	start: string;
	durationMin: number;
	/** Last day (YYYY-MM-DD) an occurrence may start on. */
	until: string;
	/** Days (YYYY-MM-DD) on which the occurrence is skipped. */
	skip?: string[];
};

export type Slot = { start: number; end: number };

export const MAX_OCCURRENCES = 520;

export function occurrences(rule: WeeklyRule): Slot[] {
	const first = parseInstant(rule.start);
	const until = parseInstant(`${parseDay(rule.until)}T00:00:00Z`);
	if (!Number.isInteger(rule.durationMin) || rule.durationMin <= 0) {
		throw new BookingError("INVALID_RANGE", `durationMin must be a positive integer, got ${rule.durationMin}`);
	}
	const skip = new Set((rule.skip ?? []).map(parseDay));
	const slots: Slot[] = [];
	for (let t = first; t < until + DAY_MS; t += 7 * DAY_MS) {
		if (slots.length >= MAX_OCCURRENCES) {
			throw new BookingError("INVALID_RANGE", `a series may not have more than ${MAX_OCCURRENCES} occurrences`);
		}
		if (skip.has(dayKey(t))) continue;
		slots.push({ start: t, end: t + rule.durationMin * MINUTE_MS });
	}
	return slots;
}
