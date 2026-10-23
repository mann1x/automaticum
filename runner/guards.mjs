// Guard trips as the SDK announces them: a "notice" event whose metadata
// names the guard (kind) and what it did (phase). Read from the published
// bundle of 4.100.258; the SDK's own names are kept.
//
// An arm with guards.stop_at_first_trip ends the run at the first trip
// (owner, 2026-10-23, D99): a guard is what signals a loop, and a run that
// is stopped there does not go round again.

// GUARD_KINDS are the notice kinds that are a guard's action, with what the
// SDK does there.
//   reasoning_loop       the reasoning-loop guard: "cut" (the stream is cut
//                        and the request made again), "aborted" (the streak
//                        of cuts ended the run), "removed" (a repeated passage
//                        taken out of the reasoning after the turn)
//   tool_intent_overrun  the tool-intent guard: "cut"
// The tool-call loop guard (loop_detection), the reasoning-repetition nudge,
// the tool-argument loop guard, strong nudges, the stale-read guard and edit
// verification announce nothing in 4.100.258: they are asked of the SDK
// under these names, and counted once it sends them.
export const GUARD_KINDS = new Set([
	"reasoning_loop",
	"tool_intent_overrun",
	"loop_detection",
	"reasoning_repetition",
	"tool_argument_loop",
	"strong_nudge",
	"stale_read_rewrite",
	"edit_verification",
]);

// guardTrip returns the trip an agent event announces, or null.
export function guardTrip(event) {
	if (event?.type !== "notice") return null;
	const m = event.metadata;
	if (!m || !GUARD_KINDS.has(m.kind)) return null;
	return {
		guard: m.kind,
		phase: typeof m.phase === "string" ? m.phase : "",
		iteration: Number.isInteger(m.iteration) ? m.iteration : null,
		message: typeof event.message === "string" ? event.message : "",
	};
}
