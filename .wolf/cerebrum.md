# Cerebrum

> OpenWolf's learning memory. Updated automatically as the AI learns from interactions.
> Do not edit manually unless correcting an error.
> Last updated: 2026-10-12

## User Preferences

<!-- How the user likes things done. Code style, tools, patterns, communication. -->
- When the owner lists setup steps, do them in the order given (repo creation before tool inits).
- GitHub repos go under the `mann1x` account.
- The repo is public: no host nicknames, private paths or addresses in tracked files.
- Never put spike or store data on the development NVMe (filling it blocks every project) or on the old harness's spool NVMe (heavily used). Use the dedicated disk recorded in docs/local/hosts.local.md.
- C# is a primary language for the owner: dotnet leads the language layer list.
- An **Open** spec item becomes a decision only when the owner says so; ask, do not decide by default. The owner is willing to answer open questions in batches when asked.

## Key Learnings

- **Project:** automaticum — Go test harness for coding agents built on @cerebriline/sdk; spec stage, no code yet.
- Source of truth: the spec set docs/specs/000..110 plus decisions.md (draft 0.5). Inputs, not edited: docs/specs/archive/ (draft spec 0.4 from the Cerebriline session, first user; omnimergekit requirements 0.1, second user).
- Host mapping, old-harness paths and mailbox aliases live in docs/local/hosts.local.md (untracked). Published files use neutral host names (linux-gpu-1, linux-gpu-2, windows-1, mac-1) and no absolute paths. The old harness is READ-ONLY.

## Do-Not-Repeat

<!-- Mistakes made and corrected. Each entry prevents the same mistake recurring. -->
<!-- Format: [YYYY-MM-DD] Description of what went wrong and what to do instead. -->
- [2026-10-12] Ran the tool inits before creating the GitHub repo although the owner listed the repo first. Follow the listed order.

## Decision Log

<!-- Significant technical decisions with rationale. Why X was chosen over Y. -->
- [2026-10-12] Owner: Go daemon; hosts run only the Go binary; Node runner inside the lane image; Docker lanes (slim base + language layers); Gitea for git tasks.
- [2026-10-12] Owner: spec-only outcome for now; agent mode only for remote hosts; TOML with tasks in this repo; one operator plus assistants; verdicts PASS/FAIL/TIMEOUT/ABORTED/INVALID/SKIPPED; raw streams as content-addressed blobs on disk; runner at full extension parity; first language layers dotnet (C#), node, python, go, rust, c/c++.
- [2026-10-12] Owner prefers store shape 3: DuckDB (state, config, metrics, stats) + Lance via lancedb-go (chunks, vectors, FTS). To be confirmed by the spike in spec 10.1.
- [2026-10-12] Owner: paused time excluded from timing; oracle sampling optional and off by default; no default for guards or compaction (arm must state each); runner installed into external harness task containers; omk_eval always owns serving in mode C; BFCL as two cells (proxy, agent); completion-form tasks stay out of automaticum; other-language SWE-bench sets are external suites with C/C++ first; detector library in Go in automaticum; VS Code-only behaviour emulated where an SDK hook exists; full TUI dashboard.
- [2026-10-12] First languages recorded as C#, C/C++, Python, TypeScript, Go, Rust. This is a reading of two answers (C# named primary, then not ticked in the language question); see decisions.md D15 note.
- [2026-10-12] Full list with ids lives in docs/specs/decisions.md; keep it and this log in step.
- [2026-10-12] Owner (second pass): SDK fixes approved on the Cerebriline side (X1-X3 first); store spike first; milestone order approved, milestone 1 planned next; one Gitea per daemon; blobs archived automatically by age; plugins compiled in only; TUI has all four view groups; runner only observes endpoint-error retries (X5 withdrawn); native agent's Node question deferred to milestone 7.
- [2026-10-12] Owner: archived logs must stay searchable like the episodic memory in claude-hooks (index stays in the database, only raw blobs move, transparent open of archived blobs, embedder version per chunk). Spec 080 section 5.1.
- [2026-10-12] Owner (milestone 1 plan): Apache-2.0; Lance binding chosen by the spike (lancedb-go v0.1.2 is unusable as released); both engines static with dynamic glibc, built on Debian 11; store required on Linux only, Windows gets agent mode and CLI without it; thresholds T1-T15 (T1-T3 hard gates); spike data on a dedicated NVMe (never the dev or spool disk), archive on the large array.
- [2026-10-12] Cerebriline SDK: X1-X3 are on its main branch, unreleased (resolved-config callback that can refuse a session, context window field, process-wide data dir option, ignore stored provider profile). No switch for the global settings file; three bundled media skills are on by default.
