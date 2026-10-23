---
paths:
  - internal/ingest/wire.go
  - internal/ingest/wirearm.go
  - internal/ingest/openai_test.go
  - internal/ingest/ingest_test.go
---

# Wire detector conventions

- `ReadWire` in `internal/ingest/wire.go` normalises each request: an OpenAI-compatible body (`openAIRequest`, `q.OpenAI == true`) is rewritten into the same `q.Options` names an Ollama request has (`reasoning_effort` becomes `think`, `reasoning_budget_tokens` becomes `options.think_budget_tokens`), and `messages` and `tools` are dropped from `q.Body`. A request field the arm cannot state is moved out of the body into `q.Options`, never left in both.
- `CompareWire` in `internal/ingest/wirearm.go` compares `q.Options` with the resolved arm. Every option the SDK may send that an arm does not state by name goes in the `SentNotStated` loop (`think_budget`, `think_budget_message`, `think_budget_tokens`), so a budget the tag owns is reported, and an effort sent as a budget is not `StatedNotSent`.
- Any change to what `CompareWire` reports bumps `WireArmVersion` in `internal/ingest/wirearm.go` (now `"3"`) and gets a dated entry in `docs/specs/070-diagnostics.md` section 4.1; `AnalysisVersion` in `internal/ingest/analyse.go` lists the detector versions.
- Prove a new wire shape with a test in `internal/ingest/openai_test.go` (OpenAI-compatible, llama-server, opencoti) or `internal/ingest/ingest_test.go` (Ollama): build the line, run `ReadWire`, assert on `q.Options` and `q.Body`, then `CompareWire` once with an arm that states the value and once with the tag owning it.
- Thresholds, provider names and option names come from the installed SDK build or the stub, never from memory: the `opencoti` and `openai-compatible` providers send a thinking effort as `reasoning_budget_tokens` (bug-029), half the output limit.
