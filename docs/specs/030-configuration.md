# 030 — Configuration

**Status:** draft 0.8, 2026-10-13.

## 1. Rules

**Decided by the owner (2026-10-12):** configuration is **TOML**, and tasks
live in this repository under `tasks/`. Large external suites are fetched at a
pinned version, not vendored.

- Files are declarative, versioned, validated on load, and resolved into the
  run record.
- **No environment-variable knobs.** Anything that changes behaviour is a
  field in a file or an explicit argument that is recorded.
- Unknown fields are an error, not a warning. A typo must not become a silent
  default.
- Validation is a command (`automaticum arm validate`, `task verify`,
  `doctor`) and runs before anything is queued.

**Decided by the owner (2026-10-13):** the daemon reads **one configuration
directory, given by `--config`**. Lanes and endpoints hold addresses and
device bindings, so that directory is local to an installation and is not
tracked; the repository carries `examples/config/` with neutral values. Arms
and tasks are tracked. An arm names an endpoint and never an address.

```
<config>/                 local, not tracked
  automaticum.toml        daemon: data root, archive root, bind address, token, [notify] webhook
  lanes/*.toml
  endpoints/*.toml
arms/*.toml               tracked
tasks/<suite>/<task>/task.toml   tracked
images/                   tracked: base and language layer definitions
campaigns/*.toml          tracked (milestone 4)
suites/*.toml             tracked (milestone 5): source, pinned version, scorer
```

## 2. Lanes

Name, host, image (base plus language layers), resource limits, the GPU devices
it is bound to, the endpoints it may use, concurrency (default 1), labels
(`gpu=...`, `os=windows`).

A lane has a state: `idle`, `running`, `paused`, `draining`, `offline`,
`quarantined` (repeated infrastructure failures).

## 3. Images

- **One slim base:** the runner, Node, git, the common shell tools the agent's
  tools call, and the sandbox prerequisites (user namespaces on Linux need a
  sysctl on some hosts).
- **One layer per language toolchain**, composable. A task declares the layers
  it needs; the daemon builds or pulls the composition and records the digest.
- Each language layer also carries that language's **language server**, used
  to give the agent diagnostics after edits (040 §6).
- Images are built by automaticum from definitions in this repository,
  reproducibly, and are pinned by digest in every run record.

**Decided by the owner (2026-10-12):** the first language layers are **C#
(dotnet), C/C++, Python, TypeScript/Node, Go and Rust**. C# is a primary
language. Java, PHP and Ruby follow. (See the note on this decision in
`decisions.md`, D15.)

## 4. Endpoints

Configured separately from lanes: kind (Ollama, xOllama, opencoti, llama.cpp,
any OpenAI-compatible, cloud), base URL, credentials by reference, log source
(local journal, agent mode on the endpoint's host, or an API), how to probe it,
how to ask it to load or unload a model, capacity (sessions served at once).

- A **capability probe** per kind, and a **real generation probe**: a
  successful pull of a cloud tag proves nothing; one token does.
- Detection of what the endpoint did to us: admission refusals (429 with
  Retry-After), context-size errors, evictions, a restart mid-run, an
  auto-update that changed the binary.
- Health is never probed in a way that disturbs a busy server.
- An endpoint may be shared by lanes on different hosts; the scheduler respects
  its capacity.
- An endpoint may be **given by the caller** for one cell (mode C, 065). It is
  probed and gated like any other, and automaticum never loads, unloads, starts
  or stops anything on it.

## 5. Arms

Every parameter that has had to be varied or verified is an arm field.

**Model and provider:** provider, model, base URL, model digest pin, context
window (including the lead agent's `num_ctx`), output budget, thinking level and
budget, sampler (temperature, top-p, top-k, min-p, repeat penalty,
repeat-last-n, presence penalty, seed) and its name, stop sequences.

**Ownership** is stated for the sampler and for thinking: `tag` (send nothing;
the model tag's own values apply) or `arm` (send these, and verify they were
sent). An arm that leaves ownership unstated fails validation.

**Agent:** prompt template family, system prompt variant, mode (plan or act),
tool selection, tool result cap, read limit, max iterations, mistake limit,
retries, timeouts (run, idle, per request), compaction (strategy, thresholds,
council, continuation), change protocol, edit verification, task-progress
interval, each guard individually (reasoning loop, tool-intent, struggle
detector, stalled-check, no-tool-call nudge, unreadable-call repair),
escalation (expert model, limits, close-after), sub-agents (enabled, nodes,
window share, swarm, teammates), sandbox mode, skills, media tools, Jev,
library, memory, checkpoints, diagnostics after edits, approval policy and the
scripted user (040 §6).

**Harness:** SDK build, runner version, image, oracle version, task version.

### 5.1 No defaults for interventions

**Decided by the owner (2026-10-12):** every guard and every compaction setting
must be stated in the arm or in a parent it extends. A missing one fails
validation. There is no built-in "evaluation" or "product" profile.

A reasoning-loop guard or a compaction that hides a loop makes a different arm.
The basis records each setting, and reports show which runs had a guard fire.
For a test of what the model does, the arm states them off.

### 5.2 Extending and resolving

- An arm file may **extend** another and override fields. The resolved arm is
  what is stored, with the origin of every value.
- `automaticum arm diff A B` prints exactly the fields that differ.
- An arm's identity is the hash of its resolved content, so two files that
  resolve to the same values are the same arm.
- **As built in milestone 2 (not an owner decision):** an arm file that only
  exists to be extended carries `template = true`. A template may leave values
  unstated, has no identity and is never run; the mark is not inherited, so
  every arm that extends it is checked whole. `arms/base.toml` is one.
- A child that switches a feature off (loop detection, compaction, the
  protocol, task progress, thinking or sampler ownership by the arm) also drops
  the settings of that feature it inherited. Settings stated beside the switch
  in the same file stay an error.

## 6. Campaigns

A campaign is data: batches, their order, interleaving, gates in addition to
the built-in ones, and stop conditions (after N consecutive `PASS`, on the
first `INVALID`, at a time). See 050.
