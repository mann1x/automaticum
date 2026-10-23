# 030 — Configuration

**Status:** draft 0.15, 2026-10-23.

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
models/*.toml             tracked: what each model tag is (family, size, variant)
runner/sdk/<build>/       tracked: one SDK build's manifest, lockfile and pinned bundle hash
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

- **No SDK in any image** (D66): the SDK build an arm names is provisioned
  apart and mounted when the run starts (040 §3.1), so one image serves
  every build and switching builds rebuilds nothing.
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
As built, an endpoint file's `kind` is `ollama`, `xollama`,
`openai-compatible` or `llamacpp` (an OpenAI-compatible llama-server, the
SDK's provider of that name); a mode C cell's endpoint is not a file but
passed on the command line (065 §3.1).

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

**Decided by the owner (2026-10-13), D79:** an endpoint of kind `ollama` or
`xollama` may state `unload_others = true`. Before a run starts, the daemon
then asks the server to drop any other model it holds in memory and waits
until it has, so that arms on different models can follow each other on one
device. It is off unless stated, because on a device shared with other work
it pushes that work's model out. The model the run uses is never dropped,
and the models dropped are in the run's record (`unloaded_before_run`).
Without the switch the gate `free` waits, as before.

### 4.1 The model catalogue

`models/*.toml`, tracked, one file per model tag (D70): `tag`, `family`,
`size`, `variant`, and optionally `lineage`, `digest` (a pin to one build of
the tag) and `notes`. It says what a tag **is**, in the owner's words. An
arm names a tag and never repeats this; every arm on a tag gets the same
identity, and an arm's own identity does not change when an entry does.
An unknown field is an error; a tag described in two files is an error.

**Decided by the owner (2026-10-13), D80:** `family` and `size` may be left
empty. On an endpoint of kind `ollama` or `xollama` they are then read from
the weights file through the server (the base model's name without its
size label, and the size label), and the run's record lists them as
`queried`. What the entry states is never replaced. The `variant` is never
read from anywhere: no file can say what a merge is called.

### 4.2 What an endpoint runs on

An endpoint file may hold a `[hardware]` table: `gpu`, `vram_gb`, `notes`
(D72). It is the owner's declaration and is recorded in every run as
declared. Endpoint files hold addresses and stay untracked (§1).

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

**Decided by the owner (2026-10-23):** `guards.stop_on` is stated too (D99,
D100): the guard trips that end the run, each a guard (`loop_detection`) or a
guard and a phase (`nudge.non_convergence`; a nudge's phase is which nudge).
The first trip it names ends the run, and the run is `FAIL` with the guard in
its reason; the oracle still runs and its answer is kept. An empty list
stops nothing: the guards act as the SDK ships them. A name the SDK does not
announce is refused (the guards and phases of 4.100.259 are in
`internal/config/arm.go`, `guardPhases`). An empty list is left out of the
resolved arm, so arms resolved before the field existed keep their
identity. Every run counts its guards' trips (`guard_trips.<guard>.<phase>`,
070).

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

### 5.3 Conditions an arm accepts

**Decided by the owner (2026-10-13), D73:** a run whose model is loaded
again in its middle, or sits only partly on the GPU, is `INVALID`
(070 §4.1). An arm that tests exactly that says so:

```toml
[accepts]
model_reload = true
partial_gpu = true
```

Each is false when left out. An arm that states one is another arm, with
another identity; an arm without the section keeps the identity it had.
With `partial_gpu`, the `gpu_residency` gate lets the run start with the
model partly on the GPU.

## 6. Campaigns

A campaign is data: batches, their order, interleaving, gates in addition to
the built-in ones, and stop conditions (after N consecutive `PASS`, on the
first `INVALID`, at a time). See 050.

**As built (milestone 4, 2026-10-14):** `campaigns/<name>.toml`, tracked;
unknown fields are errors.

```toml
description = "A against B overnight"

[stop]                       # the whole campaign; optional
at = 2026-10-15T08:00:00+02:00

[[batch]]                    # in order; each starts when the one before has ended
name = "ab"
arms = ["arm-a", "arm-b"]
tasks = ["custom/manic_miner"]
runs = 10                    # runs of each arm on each task
order = "interleaved"        # or "blocks"
lane = "lane-1"              # optional

[batch.stop]                 # this batch only; optional
after_consecutive_pass = 5   # every arm's last 5 finished runs passed
on_invalid = true            # the first INVALID run
at = 2026-10-15T06:00:00+02:00
```

No stop condition holds unless the file sets it. `after_consecutive_pass`
in a batch of several arms means that the last N finished runs of every arm
passed; the N is the file's. A batch's condition ends the batch and the
campaign goes on; the campaign's own `at` ends everything, and its other
conditions, met in any batch, end the campaign.
