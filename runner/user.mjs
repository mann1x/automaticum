// The scripted user (040 §6.2): the task's rules, played against the
// session. It answers approval requests and questions, and acts at a turn,
// after a time or when the agent ends the task. Every action it takes is
// one "user" line, so the record shows what the "user" did and when.
//
// Turn and elapsed rules act before the next model request: the tools of
// the turn before have run, and nothing is changed under a running tool.

// The sentence the extension and the SDK send for a denial.
export const DENIED = "This tool call was rejected by the user and not executed.";

// createUser plays job.user, or, when the task has none, the default user
// of protocol 1: every tool approved, every question given the task's
// default answer.
export function createUser(job, { out, act }) {
	const script = job.user ?? null;
	const rules = (script?.rules ?? []).map((r, i) => ({ ...r, index: i + 1, used: 0, re: r.match ? new RegExp(r.match) : null }));
	const answer = script ? script.default.answer : job.default_answer;
	const counts = { approvals: 0, denials: 0, questions: 0, actions: 0 };
	let turn = 0;
	let resumeWith = null;

	const free = (r) => r.times === 0 || r.used < r.times;
	const fire = (r, event, fields = {}) => {
		r.used++;
		counts.actions++;
		out("user", { on: event, rule: r.index, do: r.do, ...fields });
	};
	const denial = (feedback) => (feedback ? `${DENIED} The user provided the following feedback:\n<feedback>\n${feedback}\n</feedback>` : DENIED);

	return {
		// scripted says whether approvals go to the user: only a script asks
		// for them; without one, every tool runs without asking, as before.
		scripted: script !== null,
		// continues says whether the user may speak after a run of the
		// session ends (a resume after a cancel, a message after a
		// completion): the session must then stay open, which only an
		// interactive one does.
		continues: rules.some((r) => r.do === "cancel" || r.on === "completion"),
		counts,

		approval(request) {
			const input = JSON.stringify(request.input ?? null);
			const r = rules.find((r) => r.on === "approval" && free(r) && (!r.tool || r.tool === request.toolName) && (!r.re || r.re.test(input)));
			const approved = r ? r.do === "approve" : script.default.approval === "approve";
			const feedback = r ? r.text : script.default.feedback;
			const fields = { tool: request.toolName, tool_call_id: request.toolCallId, iteration: request.iteration, approved };
			if (r) fire(r, "approval", { ...fields, feedback: approved ? undefined : feedback });
			else out("user", { on: "approval", rule: 0, do: approved ? "approve" : "deny", ...fields });
			approved ? counts.approvals++ : counts.denials++;
			return approved ? { approved: true } : { approved: false, reason: denial(feedback) };
		},

		question(question, options) {
			counts.questions++;
			const r = rules.find((r) => r.on === "question" && free(r) && (!r.re || r.re.test(question)));
			let reply = answer;
			if (r && r.do === "option") {
				// An option the agent did not offer is the default answer, and
				// the line says so.
				reply = options?.[r.option - 1] ?? answer;
				fire(r, "question", { question, options, answer: reply, option: r.option, offered: options?.length ?? 0 });
			} else if (r) {
				reply = r.text;
				fire(r, "question", { question, options, answer: reply });
			} else {
				out("user", { on: "question", rule: 0, do: "answer", question, options, answer: reply });
			}
			return reply;
		},

		// iteration is called as each turn of the lead agent starts. Turns
		// are counted over the whole run: the SDK numbers them again from 1
		// in each run of the session, after a resume or a queued message.
		iteration() {
			turn++;
		},

		// beforeModel acts on the turn and elapsed rules that are due, in
		// their order; activeMs is the run's time so far, time held left out.
		async beforeModel(activeMs) {
			for (const r of rules) {
				if (!free(r)) continue;
				const due = (r.on === "turn" && r.turn === turn) || (r.on === "elapsed" && activeMs >= r.after_s * 1000);
				if (!due) continue;
				// A turn rule acts once on its turn, whatever its times.
				if (r.on === "turn" && r.last === turn) continue;
				r.last = turn;
				fire(r, r.on, { turn, active_ms: activeMs, text: r.text, checkpoint: r.checkpoint, restore: r.restore });
				if (r.do === "cancel") resumeWith = { text: r.text, rule: r.index };
				await act(r);
			}
		},

		// next is what the user says when a run of the session has ended:
		// the resume after a cancel, or the message a completion rule queues.
		// Null when the user has nothing more to say.
		next(result) {
			if (resumeWith) {
				const r = resumeWith;
				resumeWith = null;
				out("user", { on: "resume", rule: r.rule, do: "send", text: r.text, after: result?.finishReason ?? "" });
				return r.text;
			}
			if (result?.finishReason === "completed") {
				const r = rules.find((r) => r.on === "completion" && free(r));
				if (r) {
					fire(r, "completion", { text: r.text });
					return r.text;
				}
			}
			return null;
		},

	};
}
