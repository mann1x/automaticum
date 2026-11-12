# Memory

> Chronological action log. Hooks and AI append to this file automatically.
> Old sessions are consolidated by the daemon weekly.

## Session: 2026-11-02 04:00

## Session: 2026-11-02 08:54

## Session: 2026-11-02 09:20

## Session: 2026-11-02 10:25

## Session: 2026-11-02 11:53

## Session: 2026-11-02 12:11

## Session: 2026-11-02 13:30

## Session: 2026-11-03 01:15

| Time | Action | File(s) | Outcome | ~Tokens |
|------|--------|---------|---------|--------|
| 01:23 | deployed 8b6374f on blackswan-2 (lane idle after canary stop); found+fixed findings overwrite (cap_hit/guard_trip/control_markers lost), analysis 14 | internal/ingest/analyse.go, cap_test.go, 070 | fixed | ~15k |

## Session: 2026-11-03 01:27

| Time | Action | File(s) | Outcome | ~Tokens |
|------|--------|---------|---------|--------|
| 01:53 | Created internal/detect/duplicates.go | — | ~1322 |
| 01:56 | duplicate_definitions detector (D134), threshold 3 measured on 371 blackswan-2 runs; ReadWire keeps tool calls; analysis 15, library 6 | detect/duplicates.go, ingest/duplicates.go, wire.go, 070, decisions | built | ~40k |

## Session: 2026-11-03 01:57

| Time | Action | File(s) | Outcome | ~Tokens |
|------|--------|---------|---------|--------|
| 02:14 | deployed be1ded6 on blackswan-2 per owner plan (after opencoti released gpu1; claimed the GPU, swapped, released); canary run shows cap_hit=1 duplicate_definitions=1 (stored 0) | - | done, backup_models mailed | ~5k |

## Session: 2026-11-03 04:06

| Time | Action | File(s) | Outcome | ~Tokens |
|------|--------|---------|---------|--------|
| 04:30 | mail #2128: text loop missed; root cause cut blocks dropped (no content_end); flush as Unfinished, blocks.unfinished, AV 16; 070 draft 0.28; bug-076 | internal/ingest/events.go, analyse.go, turns.go, ingest_test.go | make test green; real run fires reasoning_repetition | ~9000 |

## Session: 2026-11-03 04:18

| Time | Action | File(s) | Outcome | ~Tokens |
|------|--------|---------|---------|--------|
| 04:21 | 252ca17 pushed; replied #2129 (fix, recovery answer wire-only still unread, guard = owner/SDK, deploy only on their ask); scratch copies removed | - | done | ~1500 |
| 05:45 | backup_models asked (note on #2129): deploy 252ca17 at idle + build wire read for seq 17; built WireAnswers (AV 17, bug-077), real run: seq 17 reasoning+text fire reasoning_repetition | internal/ingest/answers.go, events.go, wire.go, analyse.go | make test green | ~15000 |

## Session: 2026-11-03 05:18

| Time | Action | File(s) | Outcome | ~Tokens |
|------|--------|---------|---------|--------|
| 05:22 | bd4fa8a built+staged on blackswan-2 (sha match); background wait for blackswan-2 gpu1 (task bi9elfb5c); mail #2130 plan | bin/*.new | waiting for r2 end | ~800 |
| 06:24 | DEPLOYED bd4fa8a on blackswan-2 (claim 8403905ecb80, idle, unit backup .pre-bd4fa8a, .prev = be1ded6, repo at bd4fa8a); detect on 7a6be4-002: reasoning_repetition=3, blocks.unfinished=4, from_wire=3; gpu1 released | - | done | ~2000 |
| 08:03 | Created internal/detect/restated.go | — | ~750 |
| 08:04 | Session end: 1 writes across 1 files (restated.go) | 0 reads | ~803 tok |
| 08:11 | Created internal/detect/repguard.go | — | ~1257 |
| 08:12 | Created internal/ingest/repguard.go | — | ~1496 |
| 08:25 | D135 repetition guard built as detector (observe): repguard.go (detect+ingest), restated.go (SDK RestatedLines port), dup defs keyed by class (max 1 measured), library 7, AV 18; run ls limit 400 (bug-078); measured 202 runs on blackswan-2 | internal/detect, internal/ingest, internal/daemon | make test green | ~40000 |
| 08:20 | Session end: 3 writes across 2 files (restated.go, repguard.go) | 0 reads | ~3753 tok |
| 08:35 | D135 stop flag: stop_on may name repetition_guard (HarnessGuards), run.GuardStop cause -> D99 FAIL, daemon watchGuard every 10s; guard pass over 202 runs: 0 negatives, 4/5 positives; 030 0.24, 070 0.30, decisions r42 | internal/config, internal/run, internal/daemon, docs/specs | make test green | ~15000 |

## Session: 2026-11-03 08:23

| Time | Action | File(s) | Outcome | ~Tokens |
|------|--------|---------|---------|--------|
| 08:26 | be009bf pushed (D135 guard + stop flag, dup defs by class max 1, run ls limit); mail #2137 margins to backup_models; deploy waits for their idle after s800; blackswan-2 work dir cleaned | - | done | ~2000 |

## Session: 2026-11-03 10:07

| Time | Action | File(s) | Outcome | ~Tokens |
|------|--------|---------|---------|--------|
| 12:20 | mail #2141 held stream (run -004 ABORTED on SDK silence 595s): point 1 is SDK's fixed 600000ms silence ceiling (4.100.266/.269 providers.js, already probes liveness); /slots on endpoint has next_token[0].n_decoded; replied #2143: will build live /slots record + held_stream detector, verdict change asked of owner | - | replied | ~3000 |
| 13:13 | Created internal/run/slots.go | — | ~1052 |
| 13:14 | Created internal/ingest/held.go | — | ~1226 |
| 13:18 | Created internal/run/held.go | — | ~987 |
| 13:18 | Created internal/ingest/held.go | — | ~643 |
| 13:22 | Session end: 4 writes across 2 files (slots.go, held.go) | 0 reads | ~4188 tok |

## Session: 2026-11-03 13:24

| Time | Action | File(s) | Outcome | ~Tokens |
|------|--------|---------|---------|--------|
| 13:31 | D136 held stream built (slots sampler, held_stream finding, run held), eb5a1f3 pushed; deployed to blackswan-2 (gpu1:shared claim, START #2147, idle swap, repo eb5a1f3, rollback .prev=bd4fa8a, unit .pre-eb5a1f3); run held -004/-008 -> FAIL (prior ABORTED); claim released | internal/run/held.go slots.go, internal/ingest/held.go, internal/daemon/api.go | deployed | ~40000 |

## Session: 2026-11-03 15:06

| Time | Action | File(s) | Outcome | ~Tokens |
|------|--------|---------|---------|--------|
| 21:16 | mail #2154 (backup_models): repetition_guard missed 1,002x 3-line unit on s1200 -007; added text rule reasoning_loop (SDK's ported rule, run's rule set + given) to GuardText; options through GuardFires/watchGuard/detect; control added; library 8, analysis 20; tests green; measurement queued on blackswan-2 after the gate (bg b6714woba) | internal/detect/repguard.go, controls.go, detect.go, internal/ingest/repguard.go, analyse.go, internal/daemon/{detect,guardwatch,daemon}.go | in progress | ~6000 |

## Session: 2026-11-03 22:15

| Time | Action | File(s) | Outcome | ~Tokens |
|------|--------|---------|---------|--------|
| 22:17 | deployed b607c2a on blackswan-2 (idle-gated, START #2155): self-test v8 37 controls, analysis 20, NRestarts=0, repo checkout b607c2a, unit backup .pre-b607c2a, bin/*.prev=eb5a1f3; detect -007 -> repetition_guard text.reasoning_loop at 5,960/31,535 (~2,592 tok, 48s); gpu1 released | blackswan-2 | done | ~3000 |

## Session: 2026-11-04 03:06

| Time | Action | File(s) | Outcome | ~Tokens |
|------|--------|---------|---------|--------|
| 16:15 | mail #2157 (backup_models): loopproxy arm/endpoint added on blackswan-2 + 3 FRs; replied #2158: FR1 exists ([daemon] private_tasks, private+canary required, export refuses private training tasks -> owner, D105); FR2 idle-on-bytes and FR3 per-request time/x-endpoint-meta -> owner as spec items | - | replied, nothing built | ~1500 |
| 17:00 | D137 export private training tasks; D138 idle counts arriving bytes (runner receiving note, protocol 10); D139 wire first byte + x-endpoint-meta, analysis 21, run timeline -requests; specs 040/050/060/070/090, decisions rev 44 | export.go, wire.mjs, runner.mjs, protocol.go, render.go, wire.go, analyse.go, timeline_cmds.go | make test ok, runner 32/32 | ~40000 |

## Session: 2026-11-04 16:36

| Time | Action | File(s) | Outcome | ~Tokens |
|------|--------|---------|---------|--------|
| 17:10 | bc5cfd9 built (automaticum bed34271, automaticumd 6af3d03d), staged on blackswan-2 bin/*.new; asked backup_models (#2162) for a deploy window: binary swap + repo checkout + rebuild 11 lane images (protocol 10) | - | waiting for OK | ~2000 |
| 17:15 | deployed bc5cfd9 on blackswan-2 (START #2164, DONE #2165): swap, repo, 11 lane images rebuilt (protocol 10), stub smoke PASS 20261104-161148-7f84c7, analysis 21; answered idle_s question | - | live | ~5000 |
| 17:35 | D140 task pin [--write] (internal/task/pin.go, check_cmds.go), D141 config check; live: task pin --write on unpinned sh_logtriage reproduced c645bdeb; specs 090 0.31, decisions | check_cmds.go, pin.go, main.go | make test ok | ~15000 |

## Session: 2026-11-04 17:18

| Time | Action | File(s) | Outcome | ~Tokens |
|------|--------|---------|---------|--------|
| 17:40 | ad170ef CLI only on blackswan-2 (daemon stays bc5cfd9, no restart); config check there: 3 old arms name missing endpoint xollama-windows-2 (client's config, reported #2166, untouched) | - | live | ~2000 |
| 17:29 | chk mail: none unread; backup_models ack on #2166: will use task pin --write; 3 xollama-windows-2 arms are from the Qwen/omnimerge work, left for the owner | - | noted | ~200 |
| 17:32 | owner: deleted 3 xollama-windows-2 arms on blackswan-2 (moved to config/arms.removed-20261104, nothing extends them); config check all ok; mailed backup_models | - | done | ~500 |

## Session: 2026-11-04 19:06

| Time | Action | File(s) | Outcome | ~Tokens |
|------|--------|---------|---------|--------|
| 11:31 | Created ../cline/sdk/packages/core/src/extensions/tools/executors/whole-file.ts | — | ~616 |
| 11:32 | Created ../cline/sdk/packages/core/src/extensions/tools/executors/whole-file.test.ts | — | ~976 |
| 11:37 | mail #2168 (backup_models): SDK sed/awk read 150MB run_tests whole -> runner OOM; owner 'fix the sdk': guard committed in cline main f79269d469 (sed/awk/grep/search_codebase refuse binary or >100MB before reading), 534 executor tests pass; not pushed/released; acked #2168 | ../cline executors | release pending owner | ~4000 |
| 13:12 | SDK 4.100.274 released (owner ok): cline f79269d469 + 659dd0ef2d, tag v4.100.274, npm 5 pkgs + VSIX; pinned here runner/sdk/4.100.274 (bundle da8f7474), sdk verify reproduced, stub arm, X13 in 040/decisions, make test ok | runner/sdk/4.100.274, examples/config/arms/stub-sdk-4.100.274.toml, specs | committed | ~2000 |

## Session: 2026-11-05 13:15

| Time | Action | File(s) | Outcome | ~Tokens |
|------|--------|---------|---------|--------|
| 13:18 | blackswan-2: repo 30c6b1e (additions only), sdk add+verify 4.100.274 under the blackswan-2 build claim, no daemon restart; container proof: 274 sed refuses 150MB + ELF, 266 passes ELF through; mailed backup_models #2170 (arm switch = their call; lane stub smoke pending a free lane) | - | done | ~1500 |
| 14:52 | owner 'integrate 4.100.274' -> 'harness default + host': D142, examples base.toml sdk_release 4.100.274, arm-stub.json regenerated, run/daemon tests moved, make test ok; host step (blackswan-2 base.toml + stub-sdk-4.100.274.toml) blocked by permission classifier, not done | examples/config/arms/base.toml, runner/test/arm-stub.json, internal/run/run_test.go, internal/daemon/daemon_test.go, specs | repo committed | ~2500 |

## Session: 2026-11-06 14:06

| Time | Action | File(s) | Outcome | ~Tokens |
|------|--------|---------|---------|--------|
| 09:30 | owner's go: blackswan-2 base.toml sdk_release 4.100.250 -> 4.100.274 (backup work/base.toml.pre-d142), stub-sdk-4.100.274 arm added; arm-id diff: only stub + stub-plan changed, all g4 arms unchanged; stub smoke 0dd068 PASS 38s on 4.100.274 (first try 8ffb60 SKIPPED: my run-id grep took the batch id, bug-081); mailed START #2179 / DONE #2180 | .wolf/buglog.json, .wolf/cerebrum.md | done | ~3000 |

## Session: 2026-11-08 03:00

| Time | Action | File(s) | Outcome | ~Tokens |
|------|--------|---------|---------|--------|
| 09:07 | Edited ../cline/sdk/packages/core/src/extensions/tools/delimiter-balance.ts | added 1 import(s) | ~25 |
| 09:07 | Edited ../cline/sdk/packages/core/src/extensions/tools/delimiter-balance.ts | modified if() | ~48 |
| 09:13 | Edited ../cline/sdk/packages/core/src/extensions/tools/delimiter-balance.ts | added optional chaining | ~107 |
| 09:13 | Edited ../cline/sdk/packages/core/src/extensions/tools/delimiter-balance.ts | 3→5 lines | ~85 |
| 09:35 | owner 'fix the harness NOW' (mail #2187): SDK delimiter scan offered wrong 'checked' edits; fixed in cline delimiter-balance.ts (parser-gated edits, stop line named, A1 ESM/TLA/shebang), tests 60/60, seed proved on node22/bun1.3/bun1.4; handed to SDK owner the SDK owner (ollama) for commit + 4.100.275 (#2192); 4-agent tool audit, 80+ findings -> docs/local/sdk-audit-2026-11-08/FINDINGS.md; mailed backup_models #2193, xollama #2188/#2194 | .wolf/buglog.json, .wolf/cerebrum.md | handed over | ~60000 |
| 09:28 | Session end: 8 writes across 5 files (delimiter-balance.ts, repair.ts.part, describe.ts.part, repair-tests.part, FINDINGS.md) | 3 reads | ~17433 tok |
| 09:29 | Session end: 8 writes across 5 files (delimiter-balance.ts, repair.ts.part, describe.ts.part, repair-tests.part, FINDINGS.md) | 3 reads | ~17433 tok |
| 09:44 | mail #2195 the SDK owner (ollama): owner holds 4.100.275 until all ~80 audit findings are fixed; delimiter fix committed locally f7426281ef (not pushed); relayed to backup_models (gates/rollouts held); acked, hands off the cline tree | - | waiting for release | ~300 |
| 09:43 | Session end: 8 writes across 5 files (delimiter-balance.ts, repair.ts.part, describe.ts.part, repair-tests.part, FINDINGS.md) | 3 reads | ~17433 tok |
| 10:11 | Session end: 8 writes across 5 files (delimiter-balance.ts, repair.ts.part, describe.ts.part, repair-tests.part, FINDINGS.md) | 3 reads | ~17433 tok |

## Session: 2026-11-08 11:06

| Time | Action | File(s) | Outcome | ~Tokens |
|------|--------|---------|---------|--------|
| 12:11 | mail #2199 ollama: sent the full text of bash m1-m7 and document minors d1-d8 (#2200), from the audit agents' reports; FINDINGS.md expanded. blackswan-2 bc5cfd9 up 9h50m, NRestarts=0, idle, 0 queued; gpu1 95% = backup_models' bm-889 probe (exclusive claim) | docs/local/sdk-audit-2026-11-08/FINDINGS.md | replied | ~3000 |
| 14:30 | ollama #2202/#2204/#2205: 4.100.275 released (da478ecc8a, all FINDINGS fixed), not on npm; sent N2/N4/N7/N8 text (#2206). Pinned local build runner/sdk/4.100.275+da478ecc8a (packs kept in docs/local/sdk-4.100.275, bundle 4ac9a070, sdk verify reproduced), stub arm stub-sdk-4.100.275 (baf642b9a051), X14 in 040/decisions; core loop guard now forgets verdicts when named files change (CallGuard replay D129 does not model it yet); make test ok | runner/sdk/4.100.275+da478ecc8a, examples/config/arms/stub-sdk-4.100.275.toml, docs/specs, CLAUDE.md | pinned | ~6000 |

## Session: 2026-11-08 14:07

| Time | Action | File(s) | Outcome | ~Tokens |
|------|--------|---------|---------|--------|
| 14:15 | blackswan-2: repo 30c6b1e->88c66d2, sdk add+verify 4.100.275+da478ecc8a under the build claim (bundle 4ac9a070 as pinned), stub arm in host config, stub smoke 20261108-131001-f2d7b7 PASS 3.0s; no daemon restart (NRestarts=0); claim released; mailed backup_models START #2207 / DONE #2208, acked ollama #2205. Follow-up: CallGuard replay (D129) for 4.100.275's file-stamp verdict reset | - | installed | ~2500 |
| 15:10 | owner 'fix the call-guard replay': detect library 9 — CallGuard275 (4.100.275 loop guard forgets a failure/refusal once the call's files are written; writes from writing-tool success or run_commands' changed-files note, ShellWrites), CallGuardFor, Options.CallRules from the run's release; releaseAtLeast reads local builds (bug-084: RulesFor read 4.100.275-g… as unknown); tests; 070 §3.2 library 9, X14; make test ok | internal/detect/callguard.go, guards258.go, detect.go, internal/ingest/detectors.go, tests, docs/specs | done | ~25000 |

## Session: 2026-11-08 14:51

| Time | Action | File(s) | Outcome | ~Tokens |
|------|--------|---------|---------|--------|
| 15:20 | built 73ca844 (library 9) and staged on blackswan-2 as bin/*.new (sha256 match); asked backup_models for a swap window (#2212); asked ollama to mail when 4.100.276 is cut (#2213); hourly tick will act on replies | - | waiting for window and .276 | ~800 |
| 15:45 | ollama #2211: 4.100.276 on npm (07731ff6fd; N2/N5b/N6b/N7b/N8). Pinned runner/sdk/4.100.276 (bundle eb0aa5c0, verify reproduced; runtime/safety + agents unchanged from 275, CallGuard275/Rules258 hold), stub arm, D143 template default 4.100.276 (owner 'integrate .276', D142 reading), X14/040/CLAUDE.md, arm-stub.json regenerated (only sdk_release differs); make test ok | runner/sdk/4.100.276, examples/config/arms, runner/test/arm-stub.json, internal/run, internal/daemon tests, docs/specs | done | ~6000 |

## Session: 2026-11-08 14:59

| Time | Action | File(s) | Outcome | ~Tokens |
|------|--------|---------|---------|--------|
| 16:00 | b497fba rebuilt and restaged on blackswan-2 (sha256 match); mailed backup_models #2214 (window: swap + host base.toml -> 4.100.276; only stub/stub-plan inherit base, every g4 arm pins 262/266 via g4-128e/shipped); waiting on the blackswan-2 build claim (opencoti, shared) for sdk add 4.100.276 | - | waiting | ~1500 |
| 16:12 | blackswan-2 under the blackswan-2 build claim: repo ff to b497fba, sdk add+verify 4.100.276 (bundle eb0aa5c0 as pinned), stub arm, stub smoke 20261108-141122-11a150 PASS 3.2s; no restart; claim released. Swap to b497fba + host base.toml 4.100.276 still waiting for backup_models' window (#2214) | - | installed | ~1200 |
| 16:55 | owner 'not in use' + backup_models go #2215: blackswan-2 swapped bc5cfd9 -> b497fba (library 9, analysis 21; .prev kept, unit backup .pre-b497fba), host base.toml 4.100.274 -> 4.100.276 (backup work/base.toml.pre-d143); arm ids: only stub + stub-plan changed, 38 unchanged; stub smoke 20261108-145359-8a607b PASS on 4.100.276; NRestarts=0; claim released; START #2216, DONE follows | - | deployed | ~2000 |

## Session: 2026-11-09 04:00

| Time | Action | File(s) | Outcome | ~Tokens |
|------|--------|---------|---------|--------|
| 15:10 | Created internal/daemon/reload.go | — | ~2783 |
| 15:30 | owner-reported defect (no live config reload): D144 built — config.Local accessors + Reload/Apply, daemon Reload (SIGHUP, POST /v1/config/reload, watch_config watcher), listen rebind, token live, retiring lanes, waiting runs, panic isolation, signals HUP/USR1/USR2/PIPE from first moment, CLI config reload/show, LoadClient; tests + real-binary run; bug-085..087 | internal/config, internal/daemon, cmd/automaticumd, cmd/automaticum, specs 030/050/090, decisions | built, make test green | ~60000 |

## Session: 2026-11-09 15:28

| Time | Action | File(s) | Outcome | ~Tokens |
|------|--------|---------|---------|--------|
| 15:32 | 455b77f pushed (D144 live reload); staged on blackswan-2 as bin/*.new (sha256 match), config check ok with the new build; swap waits for an idle window from backup_models (#2222); lane-1 busy with their sft9-s50 run | - | staged | ~800 |
| 15:46 | blackswan-2 swapped to 455b77f (idle-gated, backup_models #2223): bin/*.prev kept, unit +ExecReload (.pre-455b77f), repo 455b77f; up, NRestarts=0, 6 lanes / 25 endpoints, sft9-s100 on lane-2 and sft9-s150 on lane-1 loaded; systemctl reload = SIGHUP reload ok; version label reads b497fba-dirty (built before the commit; code is 455b77f) | - | deployed | ~1500 |
| 21:20 | bug-088 (backup_models #2231, owner): run ls -arm/-model now find queued and running runs (arm they started with / would start with now), run ls -state; GET /v1/arms and arm ls with RUNNING/QUEUED/LANES, -active, -running; TestActiveArms, TestMergeArms; 090 0.33 | internal/daemon/arms.go, api.go, daemon.go, cmd/automaticum/show_cmds.go, client_cmds.go | make test green | ~9000 |
| 21:23 | 6203930 pushed; smoke-tested the real daemon binary on a scratch config (2 queued runs listed by run ls -arm/-model/-state and arm ls -active; clean drain); SDK release added to /v1/arms and queued run views |  internal/daemon/arms.go | ok | ~1500 |


## Session: 2026-11-09 21:22

| Time | Action | File(s) | Outcome | ~Tokens |
|------|--------|---------|---------|--------|
| 21:27 | 1719590 staged on blackswan-2 as bin/*.new (sha256 match), config check ok (47 arms, 24 models, 56 tasks); swap waits for a window from backup_models (#2232): 2 running, 7 queued on gatefix-g0/g1 | - | staged | ~600 |
| 23:36 | blackswan-2 swapped to 1719590 (backup_models window #2239; gpu0/gpu1 claimed and released): bin/*.prev and unit .pre-1719590 kept, repo 1719590, NRestarts=0, all lanes idle; arm ls -active/-running, run ls -arm -state finished, SIGHUP reload verified on the host | - | deployed | ~1500 |
| 23:52 | bug-089 (owner): run ls -arm-name / ?arm= / --where arm=; store schema 4 adds runs.arm_name (store.ArmName, backfilled on open); fake runner records the resolved arm; rehearsed on a copy of blackswan-2's state.duckdb: 935/935 named, shared id gate-g1|probe surfaced; deploy skill: back up state.duckdb on a schema bump | internal/store/duck/duck.go, store.go, internal/daemon/api.go, cmd/automaticum/client_cmds.go | make test + test-store green | ~12000 |
| 04:22 | blackswan-2 swapped to f6221de (schema 4, state.duckdb.pre-f6221de kept); -arm-name 5/4 vs -arm 9 on the shared id; reload ok; DONE mailed | blackswan-2 | ok | ~3k |
| 10:05 | D145 manic_miner v5 (backup_models #2262, owner): ok = runs && game_logic.ok, old meaning as runs; pinned 4088844b, oracle verify 3x3, fixture output checked; 060 §5.1, 050 §6 + 030 §1 (tasks read per run start; a run never builds artefacts: re-pin -> oracle verify per installation); bug-090 | tasks/custom/manic_miner, docs/specs | make test green | ~8k |
| 10:09 | blackswan-2 clone -> 01b0e01 after the s800 gate went idle; oracle verify manic_miner v5 proven (same artefact 4088844b, task hash b075f291 as the dev build); config check ok; DONE #2265 with the task-reload answer | blackswan-2 | deployed | ~2k |
| 10:50 | owner via #2266: v4/v5 comparable (060 §5.1, 020 §4, 065 §4, 070, D145, task.toml comment); bug-091 rescore judged run_game against the current pin -> against the run's workspace_before; compare counts rescored runs at the rescoring version; fake runner records TaskVersion | internal/daemon/rescore.go, compare.go, compare_cmds.go, specs | make test green | ~15k |
| 18:12 | D146 (owner via #2269): probe takes id/aliases//props model_alias (bug-093, two SKIPPED batches); run start preflights the endpoint and model_tag/template/sampler/thinking before queueing (--no-preflight; no answer = warning); endpoint forward_to, daemon forwards a loopback-only server (forward.go); `automaticum run server` (endpoint/arm/lane/forward/key/reload/start in one step, server sampler by default); 030 §4.3, 050 §4.1, 090 §1 | internal/run, internal/daemon, internal/config, cmd/automaticum, specs | make test green | ~40k |
| 18:40 | D147 (owner via #2270): detect --json carries every metric by default (--metric narrows, none opts out), header metrics_included, row metrics null only when not asked; totals loops = reasoning_loop cuts per model hour; 070 §3.3; bug-094 | internal/daemon/detect.go, cmd/automaticum/detect_cmds.go, specs | make test green | ~15k |
| 18:55 | D148 (owner via #2273): threshold registry (detect.Thresholds: default, origin sdk/owner, source), [thresholds] in automaticum.toml, detect --threshold/--thresholds, header thresholds + loops.thresholds; loop blocks (thinking.max_repeat/loop_blocks/loop_block_chars/loop_block_share/loop_block_threshold, default 20 owner: SDK has no such rule), Go port equals loopscan on 43 blocks; 030 §1, 070 §3.3 | internal/detect/thresholds.go, internal/ingest, internal/daemon/detect.go, cmd/automaticum/detect_cmds.go | make test green | ~30k |
| 19:40 | D148 step 2: the detector library's and ingest's numbers moved into the registry (53 thresholds; sdk/owner/harness origins with sources); Options.T / Values threaded through text detectors, SDK guards (250/258), restated, call guard, struggle, duplicates, spans, cap, canary, reload; guard watch and export read the installation's; threshold.<name> metric per overridden value; overrides proven to reach detectors | internal/detect, internal/ingest, internal/daemon | make test green | ~45k |
| 19:16 | owner: model_reloaded.min_load_seconds stays 1 s (no safe default better; edge cases set by hand); registry origin owner, "harness" origin removed; 070 §3.3, D148 | internal/detect/thresholds.go, specs | tests green | ~2k |
