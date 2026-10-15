// The scripted user (040 §6.2): the task's rules, played against the
// session. It answers approval requests and questions, and acts at a turn,
// after a time or when the agent ends the task. Every action it takes is
// one "user" line, so the record shows what the "user" did and when.
//
// Turn and elapsed rules act before the next model request: the tools of
// the turn before have run, and nothing is changed under a running tool.

// The sentences the extension sends for a denial (Cline 4.100.251,
// tool-approval-denial.ts, as the Cerebriline session read it): one for an
// edit tool, one for any other; feedback, trimmed, follows on its own line.
export const DENIED = "This tool call was rejected by the user and not executed.";
export const EDIT_DENIED = "The user denied this edit. The file was NOT modified and still contains its original content.";
// What the extension sends when the user switches to act mode without a
// message of their own (sdk-user-message-mapping.ts).
export const APPROVED_ACT = "The user approved switching to act mode. Continue with the approved plan now.";

// createUser plays job.user, or, when the task has none, the default user
// of protocol 1: every tool approved, every question given the task's
// default answer.
export function createUser(job, { out, act }) {
	const script = job.user ?? null;
	const rules = (script?.rules ?? []).map((r, i) => ({ ...r, index: i + 1, used: 0, re: r.match ? new RegExp(r.match) : null }));
	const answer = script ? script.default.answer : job.default_answer;
	const counts = { approvals: 0, denials: 0, questions: 0, actions: 0 };
	let turn = 0;
	// pending is what the user says once the run of the session it stopped
	// has ended: a resume, a mode switch or a fork.
	let pending = null;

	const free = (r) => r.times === 0 || r.used < r.times;
	const fire = (r, event, fields = {}) => {
		r.used++;
		counts.actions++;
		out("user", { on: event, rule: r.index, do: r.do, ...fields });
	};
	const categories = script?.categories ?? {};
	const categoryOf = (tool) => Object.keys(categories).find((c) => categories[c].includes(tool)) ?? "";
	const denial = (tool, feedback) => {
		const base = categoryOf(tool) === "edit" ? EDIT_DENIED : DENIED;
		const text = (feedback ?? "").trim();
		return text ? `${base} The user provided the following feedback:\n<feedback>\n${text}\n</feedback>` : base;
	};

	return {
		// scripted says whether approvals go to the user: only a script asks
		// for them; without one, every tool runs without asking, as before.
		scripted: script !== null,
		// asking are the tools that ask for approval, as in the extension:
		// those of a category; any other runs without asking.
		asking: Object.values(categories).flat(),
		// continues says whether the user may speak after a run of the
		// session ends (a resume after a cancel, a message after a
		// completion): the session must then stay open, which only an
		// interactive one does.
		continues: rules.some((r) => ["cancel", "mode"].includes(r.do) || ["completion", "plan"].includes(r.on) || (r.do === "restore" && r.restore !== "workspace")),
		counts,

		approval(request) {
			const input = JSON.stringify(request.input ?? null);
			const category = categoryOf(request.toolName);
			const r = rules.find(
				(r) => r.on === "approval" && free(r) && (!r.tool || r.tool === request.toolName) && (!r.category || r.category === category) && (!r.re || r.re.test(input)),
			);
			const approved = r ? r.do === "approve" : script.default.approval === "approve";
			const feedback = r ? r.text : script.default.feedback;
			const fields = { tool: request.toolName, category, tool_call_id: request.toolCallId, iteration: request.iteration, approved };
			if (r) fire(r, "approval", { ...fields, feedback: approved ? undefined : feedback });
			else out("user", { on: "approval", rule: 0, do: approved ? "approve" : "deny", ...fields });
			approved ? counts.approvals++ : counts.denials++;
			return approved ? { approved: true } : { approved: false, reason: denial(request.toolName, feedback) };
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
				fire(r, r.on, { turn, active_ms: activeMs, text: r.text, checkpoint: r.checkpoint, restore: r.restore, mode: r.mode });
				// A resume, a switch and a fork stop the running work first,
				// and are said when it has stopped.
				const fork = r.do === "restore" && r.restore !== "workspace";
				if (r.do === "cancel" || r.do === "mode" || fork) pending = { rule: r, after: "cancel" };
				await act(r);
			}
		},

		// next is what the user says when a run of the session has ended, in
		// mode: { prompt } to send, with { mode } to switch to first or
		// { fork: { checkpoint, workspace } } to restore the messages first
		// (D86). It is the second half of a cancel, a switch or a fork a
		// turn or time rule began, or what a plan or a completion rule says.
		// Null when the user has nothing more to say.
		next(result, mode) {
			const say = (r) => {
				if (r.do === "mode") return { prompt: r.text || (r.mode === "act" ? APPROVED_ACT : r.text), mode: r.mode };
				if (r.do === "restore") return { prompt: r.text, fork: { checkpoint: r.checkpoint, workspace: r.restore === "both" } };
				return { prompt: r.text };
			};
			if (pending) {
				const r = pending.rule;
				pending = null;
				const s = say(r);
				out("user", { on: "resume", rule: r.index, do: r.do === "cancel" ? "send" : r.do, text: s.prompt, mode: s.mode, after: result?.finishReason ?? "" });
				return s;
			}
			if (result?.finishReason !== "completed") return null;
			// In plan mode an end the agent chose is its plan (the extension's
			// "plan_completion_result"). An end by switch_to_act_mode never
			// comes here: the runner switches first, as the extension does.
			const on = mode === "plan" ? "plan" : "completion";
			const r = rules.find((r) => r.on === on && free(r));
			if (!r) return null;
			const s = say(r);
			fire(r, on, { text: s.prompt, mode: s.mode });
			return s;
		},

	};
}
