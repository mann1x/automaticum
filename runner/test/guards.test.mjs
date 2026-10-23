import assert from "node:assert/strict";
import { test } from "node:test";
import { guardTrip } from "../guards.mjs";

test("a guard's notice is a trip, with its guard, phase and turn", () => {
	const ev = {
		type: "notice",
		noticeType: "status",
		message: "Cutting the request and continuing.",
		metadata: { kind: "reasoning_loop", reason: "reasoning_loop", phase: "cut", iteration: 12, consecutiveTrips: 1 },
	};
	assert.deepEqual(guardTrip(ev), { guard: "reasoning_loop", phase: "cut", iteration: 12, message: "Cutting the request and continuing." });
	assert.equal(guardTrip({ ...ev, metadata: { kind: "tool_intent_overrun", phase: "cut" } }).guard, "tool_intent_overrun");
});

test("other notices and events are not trips", () => {
	// The no-tool-call nudge is the runtime's, not a guard.
	assert.equal(guardTrip({ type: "notice", metadata: { kind: "nudge", nudge: "no_tool_call", iteration: 3 } }), null);
	assert.equal(guardTrip({ type: "notice", message: "context-breakdown" }), null);
	assert.equal(guardTrip({ type: "notice", metadata: { kind: "provider_error_retry", phase: "started" } }), null);
	assert.equal(guardTrip({ type: "content_start", metadata: { kind: "reasoning_loop" } }), null);
	assert.equal(guardTrip(undefined), null);
});

test("a trip without a phase or a turn says so", () => {
	assert.deepEqual(guardTrip({ type: "notice", metadata: { kind: "loop_detection" } }), { guard: "loop_detection", phase: "", iteration: null, message: "" });
});
