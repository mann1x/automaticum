import assert from "node:assert/strict";
import { test } from "node:test";
import { guardTrip, stopsOn } from "../guards.mjs";

const notice = (metadata, message = "") => ({ type: "notice", noticeType: "status", message, metadata });

test("a guard's notice is a trip, with its guard, phase, turn, cause and tool", () => {
	const ev = notice({ kind: "reasoning_loop", reason: "reasoning_loop", phase: "cut", cause: "argument_loop", iteration: 12 }, "Cutting the request and continuing.");
	assert.deepEqual(guardTrip(ev), { guard: "reasoning_loop", phase: "cut", iteration: 12, message: "Cutting the request and continuing.", cause: "argument_loop" });
	assert.deepEqual(guardTrip(notice({ kind: "loop_detection", phase: "soft", iteration: 7, toolName: "editor" })), {
		guard: "loop_detection",
		phase: "soft",
		iteration: 7,
		message: "",
		tool: "editor",
	});
});

test("a nudge's phase is which nudge it was; the completion guards and struggle are trips too", () => {
	assert.deepEqual(guardTrip(notice({ kind: "nudge", nudge: "no_tool_call", iteration: 3 })), { guard: "nudge", phase: "no_tool_call", iteration: 3, message: "" });
	for (const kind of ["edit_verification", "checklist_close_out", "team_obligation", "struggle"]) {
		assert.equal(guardTrip(notice({ kind, phase: "nudge", attempt: 1 })).guard, kind);
	}
});

test("the arm's stop_on names a guard, or a guard and a phase", () => {
	const loops = ["loop_detection", "reasoning_loop", "tool_intent_overrun", "nudge.non_convergence", "nudge.silent_repetition"];
	const trip = (m) => guardTrip(notice(m));
	assert.equal(stopsOn(loops, trip({ kind: "loop_detection", phase: "soft" })), true);
	assert.equal(stopsOn(loops, trip({ kind: "reasoning_loop", phase: "removed" })), true);
	assert.equal(stopsOn(loops, trip({ kind: "nudge", nudge: "silent_repetition" })), true);
	assert.equal(stopsOn(loops, trip({ kind: "nudge", nudge: "no_tool_call" })), false);
	assert.equal(stopsOn(loops, trip({ kind: "edit_verification", phase: "nudge" })), false);
	assert.equal(stopsOn(["loop_detection.hard"], trip({ kind: "loop_detection", phase: "soft" })), false);
	assert.equal(stopsOn(["loop_detection.hard"], trip({ kind: "loop_detection", phase: "hard" })), true);
	// An empty list, or none, stops nothing.
	assert.equal(stopsOn([], trip({ kind: "loop_detection", phase: "soft" })), false);
	assert.equal(stopsOn(undefined, trip({ kind: "loop_detection", phase: "soft" })), false);
});

test("other notices and events are not trips", () => {
	assert.equal(guardTrip(notice({ kind: "provider_error_retry", phase: "started" })), null);
	assert.equal(guardTrip({ type: "notice", message: "context-breakdown" }), null);
	assert.equal(guardTrip({ type: "content_start", metadata: { kind: "reasoning_loop" } }), null);
	assert.equal(guardTrip(undefined), null);
});
