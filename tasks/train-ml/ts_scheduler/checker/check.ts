import { BookingError, Calendar, type WeeklyRule, formatInstant } from "../src/index.ts";

function fail(msg: string): never {
	throw new Error(msg);
}

function eq(got: unknown, want: unknown, what: string): void {
	const g = JSON.stringify(got);
	const w = JSON.stringify(want);
	if (g !== w) fail(`${what}: expected ${w}, got ${g}`);
}

function expectCode(fn: () => unknown, code: string, what: string): void {
	let caught: unknown;
	try {
		fn();
	} catch (e) {
		caught = e;
	}
	if (caught === undefined) fail(`${what}: expected BookingError ${code}, nothing was thrown`);
	if (!(caught instanceof BookingError)) {
		fail(`${what}: expected BookingError ${code}, got ${(caught as Error)?.name ?? typeof caught}: ${(caught as Error)?.message ?? caught}`);
	}
	if (caught.code !== code) fail(`${what}: expected code ${code}, got ${caught.code} (${caught.message})`);
}

function noThrow<T>(fn: () => T, what: string): T {
	try {
		return fn();
	} catch (e) {
		fail(`${what}: unexpected ${(e as Error)?.name}: ${(e as Error)?.message}`);
	}
}

const starts = (list: { start: number }[]) => list.map((b) => formatInstant(b.start));

function case_(name: string, fn: () => void): void {
	try {
		fn();
		console.log(`TEST ${name} PASS`);
	} catch (e) {
		const msg = (e as Error)?.message ?? String(e);
		console.log(`TEST ${name} FAIL ${msg.split("\n")[0]}`);
	}
}

const tests: [string, () => void][] = [
	["series_until_inclusive", () => {
		const rule: WeeklyRule = { start: "2024-04-01T09:00:00Z", durationMin: 60, until: "2024-04-22" };
		const cal = new Calendar();
		const made = noThrow(() => cal.bookSeries("oak", rule, "standup"), "bookSeries until 2024-04-22");
		eq(starts(made), ["2024-04-01T09:00:00Z", "2024-04-08T09:00:00Z", "2024-04-15T09:00:00Z", "2024-04-22T09:00:00Z"],
			"weekly from 2024-04-01T09:00Z until 2024-04-22");
		const cal2 = new Calendar();
		const fewer = noThrow(() => cal2.bookSeries("oak", { ...rule, until: "2024-04-21" }, "standup"), "bookSeries until 2024-04-21");
		eq(starts(fewer), ["2024-04-01T09:00:00Z", "2024-04-08T09:00:00Z", "2024-04-15T09:00:00Z"],
			"weekly from 2024-04-01T09:00Z until 2024-04-21");
	}],
	["series_skip_utc", () => {
		const cal = new Calendar();
		const made = noThrow(() => cal.bookSeries("elm", {
			start: "2024-03-05T01:30:00Z", durationMin: 30, until: "2024-03-26", skip: ["2024-03-12"],
		}, "late sync"), "bookSeries with skip");
		eq(starts(made), ["2024-03-05T01:30:00Z", "2024-03-19T01:30:00Z", "2024-03-26T01:30:00Z"],
			"weekly 01:30Z from 2024-03-05 until 2024-03-26 skipping 2024-03-12");
	}],
	["agenda_utc_day", () => {
		const cal = new Calendar();
		cal.book("oak", "2024-03-10T02:00:00Z", "2024-03-10T03:00:00Z", "night ops");
		cal.book("oak", "2024-03-10T23:30:00Z", "2024-03-11T00:30:00Z", "late call");
		eq(cal.agenda("oak", "2024-03-10").map((b) => b.who), ["night ops", "late call"], "agenda(oak, 2024-03-10)");
		eq(cal.agenda("oak", "2024-03-09").map((b) => b.who), [], "agenda(oak, 2024-03-09)");
	}],
	["agenda_offset_input", () => {
		const cal = new Calendar();
		cal.book("oak", "2024-03-11T00:30:00+02:00", "2024-03-11T01:30:00+02:00", "berlin");
		eq(cal.agenda("oak", "2024-03-10").map((b) => b.who), ["berlin"], "agenda(oak, 2024-03-10) for a 00:30+02:00 booking");
		eq(cal.agenda("oak", "2024-03-11").map((b) => b.who), [], "agenda(oak, 2024-03-11) for a 00:30+02:00 booking");
	}],
	["back_to_back", () => {
		const cal = new Calendar();
		cal.book("oak", "2024-03-11T10:00:00Z", "2024-03-11T11:00:00Z", "ana");
		noThrow(() => cal.book("oak", "2024-03-11T11:00:00Z", "2024-03-11T12:00:00Z", "ben"), "booking 11:00-12:00 after 10:00-11:00");
		noThrow(() => cal.book("oak", "2024-03-11T09:00:00Z", "2024-03-11T10:00:00Z", "cy"), "booking 09:00-10:00 before 10:00-11:00");
		eq(cal.all().length, 3, "number of bookings");
	}],
	["overlap_detected", () => {
		const cal = new Calendar();
		cal.book("oak", "2024-03-11T10:00:00Z", "2024-03-11T11:00:00Z", "ana");
		for (const [s, e] of [["10:30", "11:30"], ["09:30", "10:01"], ["10:15", "10:45"], ["09:00", "12:00"], ["10:00", "11:00"], ["10:59", "11:00"]]) {
			expectCode(() => cal.book("oak", `2024-03-11T${s}:00Z`, `2024-03-11T${e}:00Z`, "x"), "CONFLICT", `booking ${s}-${e} over 10:00-11:00`);
		}
		eq(cal.all().length, 1, "number of bookings after rejected clashes");
	}],
	["other_room", () => {
		const cal = new Calendar();
		cal.book("oak", "2024-03-11T10:00:00Z", "2024-03-11T11:00:00Z", "ana");
		noThrow(() => cal.book("elm", "2024-03-11T10:00:00Z", "2024-03-11T11:00:00Z", "ben"), "same slot in another room");
	}],
	["series_all_or_nothing", () => {
		const cal = new Calendar();
		cal.book("oak", "2024-04-15T09:30:00Z", "2024-04-15T10:30:00Z", "review");
		expectCode(() => cal.bookSeries("oak", { start: "2024-04-01T09:00:00Z", durationMin: 60, until: "2024-04-29" }, "standup"),
			"CONFLICT", "series whose third occurrence clashes");
		eq(cal.all().length, 1, "bookings after a rejected series");
	}],
	["series_adjacent_ok", () => {
		const cal = new Calendar();
		cal.book("oak", "2024-04-08T10:00:00Z", "2024-04-08T11:00:00Z", "review");
		const made = noThrow(() => cal.bookSeries("oak", { start: "2024-04-01T09:00:00Z", durationMin: 60, until: "2024-04-15" }, "standup"),
			"series ending at 10:00 next to a 10:00 booking");
		eq(made.length, 3, "occurrences booked");
	}],
	["cancel_unknown", () => {
		const cal = new Calendar();
		cal.book("oak", "2024-03-11T10:00:00Z", "2024-03-11T11:00:00Z", "ana");
		cal.book("oak", "2024-03-12T10:00:00Z", "2024-03-12T11:00:00Z", "ben");
		expectCode(() => cal.cancel("b99"), "NOT_FOUND", "cancel(\"b99\")");
		eq(cal.all().map((b) => b.who), ["ana", "ben"], "bookings after cancel(\"b99\")");
	}],
	["cancel_then_rebook", () => {
		const cal = new Calendar();
		const a = cal.book("oak", "2024-03-11T10:00:00Z", "2024-03-11T11:00:00Z", "ana");
		cal.book("oak", "2024-03-12T10:00:00Z", "2024-03-12T11:00:00Z", "ben");
		const removed = noThrow(() => cal.cancel(a.id), "cancel of an existing booking");
		eq(removed?.who, "ana", "cancel() return value .who");
		eq(cal.all().map((b) => b.who), ["ben"], "bookings after cancel");
		noThrow(() => cal.book("oak", "2024-03-11T10:00:00Z", "2024-03-11T11:00:00Z", "cy"), "rebooking a cancelled slot");
		expectCode(() => cal.cancel(a.id), "NOT_FOUND", "cancelling the same id twice");
		eq(cal.all().length, 2, "bookings after the second cancel");
	}],
	["invalid_range", () => {
		const cal = new Calendar();
		expectCode(() => cal.book("oak", "2024-03-11T10:00:00Z", "2024-03-11T10:00:00Z", "x"), "INVALID_RANGE", "zero-length booking");
		expectCode(() => cal.book("oak", "2024-03-11T10:00:00Z", "2024-03-11T09:00:00Z", "x"), "INVALID_RANGE", "booking ending before it starts");
		expectCode(() => cal.book("oak", "2024-03-11 10:00", "2024-03-11T11:00:00Z", "x"), "INVALID_TIME", "booking without an offset");
		eq(cal.all().length, 0, "bookings after rejected ranges");
	}],
];

for (const [name, fn] of tests) case_(name, fn);
