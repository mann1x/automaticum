---
paths:
  - internal/run/probe.go
  - internal/run/props.go
  - internal/run/props_test.go
  - internal/gate/gate.go
  - internal/stubmodel/stubmodel.go
---

# Endpoint probe and gate conventions

- A `llamacpp` endpoint is probed in two steps in `internal/run/probe.go`: `/v1/models` must list the arm's model, then `ReadProps` (in `internal/run/props.go`) reads llama-server's `/props` and `fillFromProps` records the build, the template hash and bytes, what the template can do, the default sampler and window, and the weights file. A server that does not answer `/props` stays not probed; it is not an error.
- Anything the server does not report is named in `Identity.NotProbed` (for example `"template capabilities"`, `"model digest"`, `"residency"`). A gate in `internal/gate/gate.go` whose input is in `NotProbed` returns `Unchecked` with the reason, never `Refuse` (bug-028).
- The sampler settings taken from `/props` are the ones an arm can state (`llamaSampler` in `internal/run/probe.go`, mirroring `config.Sampler`); do not record others.
- A new `/props` field goes in the `raw` struct of `ReadProps`, in `Props` (`internal/run/props.go`) with an `omitempty` tag, in `fillFromProps`, and in the stub's `/props` answer in `internal/stubmodel/stubmodel.go` (it already carries `chat_template_caps` so the template gate holds on the stub). Add a row in `internal/run/props_test.go`.
- Assert on `Identity.Details` and other `json.RawMessage` fields by unmarshalling into a struct, never by substring: Go's `json.Marshal` escapes `<`, `>` and `&`.
