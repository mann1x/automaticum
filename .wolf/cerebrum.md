# Cerebrum

> OpenWolf's learning memory. Updated automatically as the AI learns from interactions.
> Do not edit manually unless correcting an error.
> Last updated: 2026-10-12

## User Preferences

<!-- How the user likes things done. Code style, tools, patterns, communication. -->
- When the owner lists setup steps, do them in the order given (repo creation before tool inits).
- GitHub repos go under the `mann1x` account.
- The repo is public: no host nicknames, private paths or addresses in tracked files.
- C# is a primary language for the owner: dotnet leads the language layer list.
- An **Open** spec item becomes a decision only when the owner says so; ask, do not decide by default.

## Key Learnings

- **Project:** automaticum — Go test harness for coding agents built on @cerebriline/sdk; spec stage, no code yet.
- Inputs: docs/specs/automaticum-draft-spec.md (draft 0.4, from the Cerebriline session, first user) and docs/specs/omnimergekit-evaluation-requirements.md (draft 0.1, from the omnimergekit session, second user).
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
