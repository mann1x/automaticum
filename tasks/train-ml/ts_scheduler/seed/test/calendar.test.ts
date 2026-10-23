import assert from "node:assert/strict";
import { test } from "node:test";
import { BookingError, Calendar, parseInstant } from "../src/index.ts";

test("parses offsets", () => {
	assert.equal(parseInstant("2024-03-10T09:00:00+02:00"), Date.UTC(2024, 2, 10, 7));
	assert.throws(() => parseInstant("2024-03-10T09:00:00"), BookingError);
});

test("books and lists a room", () => {
	const cal = new Calendar();
	cal.book("oak", "2024-03-11T10:00:00Z", "2024-03-11T11:00:00Z", "ana");
	cal.book("oak", "2024-03-11T08:00:00Z", "2024-03-11T09:00:00Z", "ben");
	assert.deepEqual(
		cal.agenda("oak", "2024-03-11").map((b) => b.who),
		["ben", "ana"],
	);
});

test("rejects a clash", () => {
	const cal = new Calendar();
	cal.book("oak", "2024-03-11T10:00:00Z", "2024-03-11T11:00:00Z", "ana");
	assert.throws(
		() => cal.book("oak", "2024-03-11T10:30:00Z", "2024-03-11T11:30:00Z", "ben"),
		(e: unknown) => e instanceof BookingError && e.code === "CONFLICT",
	);
});
