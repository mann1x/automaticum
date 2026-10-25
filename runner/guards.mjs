// Guard actions as the SDK announces them: a "notice" event whose metadata
// names the guard (kind) and what it did (phase). The table is Cerebriline's
// for 4.100.259 (mail of 2026-10-23), checked against the published bundle;
// 4.100.258 sends only the reasoning_loop, tool_intent_overrun and nudge
// notices of it.
//
// Every action is a trip and is counted. Which trips end the run is the
// arm's: guards.stop_on lists them (D100), and an empty list stops nothing.

// GUARD_KINDS are the notice kinds that are a guard's action. The same
// list is in internal/config (guardKinds), which checks an arm's stop_on.
//   loop_detection       the same tool call over and over: "soft" (first
//                        warning), "hard" (last warning), "stopped"
//   reasoning_loop       "cut" (the stream cut and the request made again),
//                        "aborted" (the streak of cuts ended the run),
//                        "removed" (a repeated passage taken out after the
//                        turn); metadata.cause is reasoning_loop, answer_loop
//                        or argument_loop
//   tool_intent_overrun  "cut"
//   nudge                the phase is which nudge: no_tool_call,
//                        unparsed_tool_call, announced_intent,
//                        non_convergence, silent_repetition
//   edit_verification, checklist_close_out, team_obligation
//                        the completion guards: "nudge"
//   struggle             "nudge", "offer"
// The stale-read rewrite announces nothing: it changes nothing the model is
// told.
export const GUARD_KINDS = new Set([
	"loop_detection",
	"reasoning_loop",
	"tool_intent_overrun",
	"nudge",
	"edit_verification",
	"checklist_close_out",
	"team_obligation",
	"struggle",
]);

// guardTrip returns the trip an agent event announces, or null. A nudge's
// phase is which nudge it was.
export function guardTrip(event) {
	if (event?.type !== "notice") return null;
	const m = event.metadata;
	if (!m || !GUARD_KINDS.has(m.kind)) return null;
	const phase = m.kind === "nudge" ? (typeof m.nudge === "string" ? m.nudge : "") : typeof m.phase === "string" ? m.phase : "";
	const trip = {
		guard: m.kind,
		phase,
		iteration: Number.isInteger(m.iteration) ? m.iteration : null,
		message: typeof event.message === "string" ? event.message : "",
	};
	if (typeof m.cause === "string") trip.cause = m.cause;
	if (typeof m.toolName === "string") trip.tool = m.toolName;
	return trip;
}

// stopsOn says whether a trip is one the arm's stop_on list names: a
// selector is a guard ("loop_detection", any phase) or a guard and a phase
// ("nudge.non_convergence").
export function stopsOn(selectors, trip) {
	if (!Array.isArray(selectors) || trip === null) return false;
	return selectors.some((s) => s === trip.guard || s === `${trip.guard}.${trip.phase}`);
}

// The reasoning-loop limit (D108). After guards.reasoning_loop
// .max_consecutive_trips turns cut one after another (absent: the SDK's own
// number, 3), the SDK asks the host whether the run ends; the answer is the
// arm's stop_on alone: stop only if it names reasoning_loop.aborted (or the
// whole guard, which has stopped the run at its first cut already).
const LIMIT_TRIP = { guard: "reasoning_loop", phase: "aborted" };

export function reasoningLimitAnswer(selectors) {
	return stopsOn(selectors, LIMIT_TRIP) ? "stop" : "continue";
}

// The SDK's own limit of turns cut, which an arm that states none has.
const SDK_LIMIT = 3;

// limitBreach says why the SDK's reasoning-loop guard did not act as the arm
// says, or null when it did: the limit it reports (4.100.266 on) is not the
// arm's; or it ended the run where the arm goes on (a release that does not
// pass the host's answer on ends it anyway); or it ended it after another
// number of cut turns than the arm states.
export function limitBreach(arm, event) {
	const m = event?.type === "notice" ? event.metadata : null;
	if (!m || m.kind !== "reasoning_loop" || (m.phase !== "aborted" && m.phase !== "cut")) return null;
	const max = arm.guards.reasoning_loop.max_consecutive_trips;
	if (Number.isInteger(m.maxConsecutiveTrips) && m.maxConsecutiveTrips !== (max ?? SDK_LIMIT)) {
		return `the SDK's reasoning-loop limit was ${m.maxConsecutiveTrips} turns cut; the arm's is ${max ?? SDK_LIMIT}`;
	}
	if (m.phase !== "aborted") return null;
	const n = Number.isInteger(m.consecutiveTrips) ? m.consecutiveTrips : null;
	const cut = n === null ? "at its limit" : `after ${n} turns cut one after another`;
	if (reasoningLimitAnswer(arm.guards.stop_on ?? []) === "continue") {
		return `the SDK ended the run for a reasoning loop ${cut}, where the arm goes on (stop_on does not name reasoning_loop.aborted, D108): this SDK release does not take the host's answer`;
	}
	if (max !== undefined && n !== null && n !== max) {
		return `the SDK ended the run for a reasoning loop ${cut}; the arm's limit is ${max}`;
	}
	return null;
}
