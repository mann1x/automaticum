# anatomy.md

> Auto-maintained by OpenWolf. Last scanned: 2026-11-14T17:00:00.315Z
> Files: 512 tracked | Anatomy hits: 0 | Misses: 0

## ./

- `.gitattributes` — Git attributes (~20 tok)
- `.gitignore` — Git ignore rules (~73 tok)
- `.gitleaks.toml` (~581 tok)
- `CALIBER_LEARNINGS.md` — Caliber Learnings (~7144 tok)
- `CLAUDE.md` — automaticum (~7556 tok)
- `go.mod` — Go module definition (~303 tok)
- `go.sum` — Go dependency checksums (~1248 tok)
- `LICENSE` — Project license (~3029 tok)
- `Makefile` — Make build targets (~809 tok)
- `README.md` — Project documentation (~2758 tok)
- `spike.local.toml` — Untracked. Paths for the store spike on this installation. (~243 tok)

## .caliber/

- `.caliber-state.json` (~35 tok)
- `learnings-archive.md` — Caliber Learnings Archive (~164995 tok)
- `manifest.json` (~728 tok)
- `refresh-hook.log` (~211 tok)
- `score-history.jsonl` (~2034 tok)

## .caliber/backups/2026-10-12T20-16-19-461Z/

- `CLAUDE.md` — OpenWolf (~57 tok)

## .caliber/backups/2026-10-12T20-16-19-461Z/.claude/rules/

- `openwolf.md` (~313 tok)

## .caliber/cache/

- `fingerprint.json` — Declares usage (~97348 tok)

## .caliber/learning/

- `current-session.jsonl` — Declares pollution (~19272 tok)
- `finalize.log` (~0 tok)
- `roi-stats.json` (~108232 tok)
- `state.json` (~41 tok)

## .claude/

- `settings.json` (~974 tok)
- `settings.local.json` (~35 tok)

## .claude/hooks/

- `caliber-check-sync.sh` — Don't block headless claude sessions spawned by caliber itself (e.g. during caliber refresh) (~722 tok)
- `caliber-session-freshness.sh` — Don't run inside a caliber-spawned headless session — the systemMessage would (~226 tok)

## .claude/rules/

- `browser.md` — The browser in the runner (D150) (~450 tok)
- `endpoint-probe.md` — Endpoint probe and gate conventions (~819 tok)
- `lane-images.md` — Lane image conventions (~1193 tok)
- `lsp.md` — Language servers in the runner (D149) (~752 tok)
- `openwolf.md` — OpenWolf protocol (~207 tok)
- `runner-protocol.md` — Runner protocol conventions (~1427 tok)
- `spec-conventions.md` — Spec conventions (~375 tok)
- `task-loader.md` — Task loader conventions (~1596 tok)
- `wire-arm.md` — Wire detector conventions (~452 tok)
- `wolf-hooks.md` — Hook conventions (~250 tok)

## .claude/skills/add-wolf-hook/

- `SKILL.md` — Add Wolf Hook (~3583 tok)

## .claude/skills/draft-spec-section/

- `SKILL.md` — Draft Spec Section (~491 tok)

## .claude/skills/find-skills/

- `SKILL.md` — Find Skills (~593 tok)

## .claude/skills/learned-detector-measurement/

- `SKILL.md` — Measuring a detector over every stored run on a lane host (~1590 tok)

## .claude/skills/learned-lane-host-deploy/

- `SKILL.md` — Deploying a new automaticum build to a lane host (~2843 tok)

## .claude/skills/learned-new-host-install/

- `SKILL.md` — Installing automaticum on a fresh host (~1437 tok)

## .claude/skills/learned-new-sdk-release/

- `SKILL.md` — Provisioning a new SDK release (~4868 tok)

## .claude/skills/log-wolf-bug/

- `SKILL.md` — log-wolf-bug (~4099 tok)

## .claude/skills/record-decision/

- `SKILL.md` — Record Decision (~522 tok)

## .claude/skills/save-learning/

- `SKILL.md` — Save Learning (~626 tok)

## .claude/skills/setup-caliber/

- `SKILL.md` — Setup Caliber (~1941 tok)

## campaigns/

- `stub-demo.toml` — A campaign on the stub model server, for trying campaigns without a GPU: (~143 tok)

## cmd/automaticum/

- `arm_ls_test.go` — TestMergeArms (~418 tok)
- `campaign_cmds.go` (~1628 tok)
- `cell_cmds.go` — exitCode: names (~3525 tok)
- `check_cmds_test.go` — TestConfigCheck (~731 tok)
- `check_cmds.go` — pin (71 fields) (~1834 tok)
- `client_cmds.go` — client (103 fields); methods: Error (~11208 tok)
- `cohort_cmds.go` (~1633 tok)
- `compare_cmds.go` — Declares list (~4140 tok)
- `config_cmds_test.go` — TestParseInterspersed, TestCellTasks, TestNoticeMatch (~593 tok)
- `config_cmds.go` — place (152 fields) (~4304 tok)
- `control_cmds.go` (~2935 tok)
- `detect_cmds_test.go` — TestDetectFailIf, TestDetectLoopTotals, TestDetectLoopBlockTotals (~1686 tok)
- `detect_cmds.go` — failTest (80 fields) (~8606 tok)
- `exit_test.go` — TestExitOf (~153 tok)
- `export_cmds.go` — result (100 fields); methods: Error (~2565 tok)
- `image_cmds.go` — built (65 fields) (~1334 tok)
- `main.go` — Command automaticum is the command line of automaticum. (~4379 tok)
- `reload_cmds.go` (~1000 tok)
- `report_cmds_test.go` — TestWriteRunsCSV, TestPositionalAndFormat (~426 tok)
- `report_cmds.go` — Declares and (~4844 tok)
- `rescore_cmds.go` (~956 tok)
- `sdk_cmds.go` — row (57 fields) (~1545 tok)
- `server_cmds_test.go` — TestPickTag, TestContainerAddress, TestAttachEndpoint, TestPlanServer, TestRunServer (~3198 tok)
- `server_cmds.go` — serverFacts (107 fields) (~7192 tok)
- `show_cmds.go` — armRow (113 fields) (~5690 tok)
- `store_cgo.go` — go:build cgo (~2147 tok)
- `store_nocgo.go` — go:build !cgo (~128 tok)
- `stub_cmds.go` (~489 tok)
- `suite_cmds.go` — plan (50 fields) (~1297 tok)
- `timeline_cmds_test.go` — TestTimeline, TestTimelineRequests (~1115 tok)
- `timeline_cmds.go` — timelineLine (44 fields) (~1500 tok)
- `wait_cmds_test.go` — oldDaemon (23 fields); methods: ServeHTTP (~984 tok)
- `wait_cmds.go` — waiter (139 fields) (~5130 tok)

## cmd/automaticumd/

- `main.go` — Command automaticumd is the daemon: it owns the store, works the queue on (~2450 tok)

## docs/audit/

- `2026-11-01-spec-vs-code.md` — Spec against code, 2026-11-01 (~27833 tok)

## docs/local/

- `hosts.local.md` — Local mapping (untracked) (~540 tok)

## docs/local/sdk-4.100.275/packs/

- `cerebriline-agents-4.100.275-gda478ecc8a.tgz` (~23573 tok)
- `cerebriline-sdk-4.100.275-gda478ecc8a.tgz` (~1303 tok)
- `cerebriline-shared-4.100.275-gda478ecc8a.tgz` (~82519 tok)
- `manifest.json` (~283 tok)
- `SHA256SUMS` (~147 tok)

## docs/local/sdk-audit-2026-11-08/

- `FINDINGS.md` — SDK tool audit, 2026-11-08 (owner order after the delimiter-scan bug) (~5529 tok)

## docs/local/sdk-audit-2026-11-08/checkers/

- `db-HEAD.ts` — Where a file's brackets stop matching. (~9784 tok)
- `jscline.cjs` — Declares fs (~113 tok)
- `repro1.ts` — Exports f, x (~649 tok)
- `repro2.ts` — Declares SRC (~206 tok)
- `repro3.ts` — SRC: tick, loop, loop + 7 more (~903 tok)
- `repro4.ts` — Declares SRC (~196 tok)
- `repro5.ts` — Exports readIt (~323 tok)
- `rt.test.ts` (~49 tok)
- `vmloc.cjs` — Declares vm (~135 tok)

## docs/local/sdk-audit-2026-11-08/checkers/fork-protocol/

- `carry-on-regression.ts` — root: a, a (~386 tok)
- `last-tx-settled.ts` — Declares root (~434 tok)
- `two-errors-after-fix1.html` — a: b (~28 tok)
- `two-errors-before.html` — a: b (~29 tok)
- `unrunnable-check-defect.ts` — Declares root (~268 tok)

## docs/local/sdk-audit-2026-11-08/checkers/fork-revisions/

- `plan.ts` — Declares c (~180 tok)
- `repro.ts` — Declares A (~1280 tok)

## docs/local/sdk-audit-2026-11-08/checkers/fork-revisions/ws/

- `a.js` (~1 tok)
- `b.js` (~1 tok)
- `c.html` (~1 tok)
- `c.js` (~1 tok)
- `d.js` (~1 tok)
- `h.js` (~1 tok)
- `n.js` (~3 tok)
- `p.js` (~1 tok)
- `t.js` (~1 tok)

## docs/local/sdk-audit-2026-11-08/checkers/w/

- `game.js` — Declares f (~4 tok)
- `rt.test.ts` (~46 tok)

## docs/local/sdk-audit-2026-11-08/delimiter-scan/

- `after-bun1.3.txt` (~1691 tok)
- `after-bun1.4.txt` (~1719 tok)
- `after-node22.txt` (~1727 tok)
- `repro2.ts` — Declares dir (~302 tok)
- `seed.html` — Manic Miner - ZX Spectrum Classic (~3767 tok)

## docs/local/sdk-audit-2026-11-08/editor/

- `parse.mjs` — Declares L (~108 tok)
- `repro1.ts` — CORE: file, run, a + 4 more (~1445 tok)
- `repro2.ts` — CORE: file, run (~917 tok)
- `repro3.ts` — Declares CORE (~384 tok)
- `repro4.ts` — Exports v (~363 tok)
- `repro5.ts` — Declares CORE (~283 tok)
- `repro5b.ts` — Declares CORE (~282 tok)
- `repro6.ts` — Declares CORE (~288 tok)
- `repro7.ts` — Declares CORE (~278 tok)

## docs/local/sdk-audit-2026-11-08/editor/work/

- `big.txt` (~1250 tok)
- `f0.ts` — a: b (~22 tok)
- `f1.ts` — Declares f (~30 tok)
- `f10.ts` — Declares price (~38 tok)
- `f100.ts` — Declares v0 (~13652 tok)
- `f101.js` — Declares A (~98 tok)
- `f2.ts` (~24 tok)
- `f3.ts` (~16 tok)
- `f4.ts` (~10 tok)
- `f5.ts` (~2 tok)
- `f6.ts` (~3 tok)
- `f7.ts` (~2 tok)
- `f8.ts` (~2 tok)
- `f9.ts` (~4 tok)
- `narrow.ts` (~82 tok)
- `reanchor.ts` (~82 tok)

## docs/local/sdk-audit-2026-11-08/editor/work/g/

- `lib.ts` — Exports v0, v1, v2, v3 + 35 more (~253 tok)

## docs/local/sdk-audit-2026-11-08/editor/work/partial/

- `a.txt` (~1 tok)
- `b.txt` (~1 tok)

## docs/local/sdk-audit-2026-11-08/readers/

- `awk1.ts` — Declares t (~298 tok)
- `awk2.ts` (~148 tok)
- `awk3.ts` — p: f (~218 tok)
- `grep1.ts` — Exports TODO_MARK (~783 tok)
- `read1.ts` — Declares T (~775 tok)
- `read2.ts` — Declares T (~414 tok)
- `search1.ts` — Exports endsHere (~248 tok)
- `search2.ts` — Declares W (~178 tok)
- `sed1.ts` — Declares W (~403 tok)

## docs/local/sdk-audit-2026-11-08/readers/bash/

- `exec.ts` — W: viaTool (~532 tok)
- `exec2.ts` — Declares W (~868 tok)
- `exec3.ts` — Declares W (~561 tok)
- `guard.ts` — Declares cases (~462 tok)

## docs/local/sdk-audit-2026-11-08/readers/bash/work/

- `a.txt` (~5 tok)
- `b.txt` (~1 tok)
- `c.txt` (~1 tok)
- `marker` (~2 tok)

## docs/local/sdk-audit-2026-11-08/readers/docs/

- `doc-repro.out` (~795 tok)
- `doc-repro.ts` — Declares W (~296 tok)
- `encrypted-docx-repro.out` (~79 tok)
- `encrypted-docx-repro.ts` — Declares CFB (~447 tok)
- `list-files-repro.out` — /package.json"} (~506 tok)
- `list-files-repro.ts` — /package.json" }); (~488 tok)
- `misc-repro.out` (~170 tok)
- `misc-repro.ts` — Declares P (~224 tok)
- `mkpdf.py` — pdf, stream, img (~707 tok)
- `web-fetch-repro.out` (~358 tok)
- `web-fetch-repro.ts` — Declares server (~585 tok)

## docs/local/sdk-audit-2026-11-08/readers/docs/ws-doc/

- `probe.epub` (~6157 tok)

## docs/local/sdk-audit-2026-11-08/readers/docs/ws-doc/.cline/extracted/SimpleWithImages/

- `SimpleWithImages.1.md` — Sheet1 (~11 tok)

## docs/local/sdk-audit-2026-11-08/readers/docs/ws-doc/.cline/extracted/form-scan/

- `form-scan.md` — Page 1 (~3 tok)

## docs/local/sdk-audit-2026-11-08/readers/docs/ws-doc/.cline/extracted/photo-caption/

- `photo-caption.md` — Page 1 (~12 tok)

## docs/local/sdk-audit-2026-11-08/readers/docs/ws-doc/.cline/extracted/pictures/

- `pictures.1.md` — Slide 1 (~3 tok)

## docs/local/sdk-audit-2026-11-08/readers/docs/ws-doc/.cline/extracted/probe/

- `probe.1.md` (~1 tok)
- `probe.99.md` (~1 tok)
- `probe.md` — Probe document (~40 tok)

## docs/local/sdk-audit-2026-11-08/readers/docs/ws-doc/.cline/extracted/tiled-scan/

- `tiled-scan.md` — Page 1 (~3 tok)

## docs/local/sdk-audit-2026-11-08/readers/docs/ws-list/many/

- `f0.log` (~0 tok)
- `f1.log` (~0 tok)
- `f10.log` (~0 tok)
- `f100.log` (~0 tok)
- `f1000.log` (~0 tok)
- `f1001.log` (~0 tok)
- `f1002.log` (~0 tok)
- `f1003.log` (~0 tok)
- `f1004.log` (~0 tok)
- `f1005.log` (~0 tok)
- `f1006.log` (~0 tok)
- `f1007.log` (~0 tok)
- `f1008.log` (~0 tok)
- `f1009.log` (~0 tok)
- `f101.log` (~0 tok)
- `f1010.log` (~0 tok)
- `f1011.log` (~0 tok)
- `f1012.log` (~0 tok)
- `f1013.log` (~0 tok)
- `f1014.log` (~0 tok)
- `f1015.log` (~0 tok)
- `f1016.log` (~0 tok)
- `f1017.log` (~0 tok)
- `f1018.log` (~0 tok)
- `f1019.log` (~0 tok)
- `f102.log` (~0 tok)
- `f1020.log` (~0 tok)
- `f1021.log` (~0 tok)
- `f1022.log` (~0 tok)
- `f1023.log` (~0 tok)
- `f1024.log` (~0 tok)
- `f1025.log` (~0 tok)
- `f1026.log` (~0 tok)
- `f1027.log` (~0 tok)
- `f1028.log` (~0 tok)
- `f1029.log` (~0 tok)
- `f103.log` (~0 tok)
- `f1030.log` (~0 tok)
- `f1031.log` (~0 tok)
- `f1032.log` (~0 tok)
- `f1033.log` (~0 tok)
- `f1034.log` (~0 tok)
- `f1035.log` (~0 tok)
- `f1036.log` (~0 tok)
- `f1037.log` (~0 tok)
- `f1038.log` (~0 tok)
- `f1039.log` (~0 tok)
- `f104.log` (~0 tok)
- `f1040.log` (~0 tok)
- `f1041.log` (~0 tok)
- `f1042.log` (~0 tok)
- `f1043.log` (~0 tok)
- `f1044.log` (~0 tok)
- `f1045.log` (~0 tok)
- `f1046.log` (~0 tok)
- `f1047.log` (~0 tok)
- `f1048.log` (~0 tok)
- `f1049.log` (~0 tok)
- `f105.log` (~0 tok)
- `f1050.log` (~0 tok)
- `f1051.log` (~0 tok)
- `f1052.log` (~0 tok)
- `f1053.log` (~0 tok)
- `f1054.log` (~0 tok)
- `f1055.log` (~0 tok)
- `f1056.log` (~0 tok)
- `f1057.log` (~0 tok)
- `f1058.log` (~0 tok)
- `f1059.log` (~0 tok)
- `f106.log` (~0 tok)
- `f1060.log` (~0 tok)
- `f1061.log` (~0 tok)
- `f1062.log` (~0 tok)
- `f1063.log` (~0 tok)
- `f1064.log` (~0 tok)
- `f1065.log` (~0 tok)
- `f1066.log` (~0 tok)
- `f1067.log` (~0 tok)
- `f1068.log` (~0 tok)
- `f1069.log` (~0 tok)
- `f107.log` (~0 tok)
- `f1070.log` (~0 tok)
- `f1071.log` (~0 tok)
- `f1072.log` (~0 tok)
- `f1073.log` (~0 tok)
- `f1074.log` (~0 tok)
- `f1075.log` (~0 tok)
- `f1076.log` (~0 tok)
- `f1077.log` (~0 tok)
- `f1078.log` (~0 tok)
- `f1079.log` (~0 tok)
- `f108.log` (~0 tok)
- `f1080.log` (~0 tok)
- `f1081.log` (~0 tok)
- `f1082.log` (~0 tok)
- `f1083.log` (~0 tok)
- `f1084.log` (~0 tok)
- `f1085.log` (~0 tok)
- `f1086.log` (~0 tok)
- `f1087.log` (~0 tok)
- `f1088.log` (~0 tok)
- `f1089.log` (~0 tok)
- `f109.log` (~0 tok)
- `f1090.log` (~0 tok)
- `f1091.log` (~0 tok)
- `f1092.log` (~0 tok)
- `f1093.log` (~0 tok)
- `f1094.log` (~0 tok)
- `f1095.log` (~0 tok)
- `f1096.log` (~0 tok)
- `f1097.log` (~0 tok)
- `f1098.log` (~0 tok)
- `f1099.log` (~0 tok)
- `f11.log` (~0 tok)
- `f110.log` (~0 tok)
- `f1100.log` (~0 tok)
- `f1101.log` (~0 tok)
- `f1102.log` (~0 tok)
- `f1103.log` (~0 tok)
- `f1104.log` (~0 tok)
- `f1105.log` (~0 tok)
- `f1106.log` (~0 tok)
- `f1107.log` (~0 tok)
- `f1108.log` (~0 tok)
- `f1109.log` (~0 tok)
- `f111.log` (~0 tok)
- `f1110.log` (~0 tok)
- `f1111.log` (~0 tok)
- `f1112.log` (~0 tok)
- `f1113.log` (~0 tok)
- `f1114.log` (~0 tok)
- `f1115.log` (~0 tok)
- `f1116.log` (~0 tok)
- `f1117.log` (~0 tok)
- `f1118.log` (~0 tok)
- `f1119.log` (~0 tok)
- `f112.log` (~0 tok)
- `f1120.log` (~0 tok)
- `f1121.log` (~0 tok)
- `f1122.log` (~0 tok)
- `f1123.log` (~0 tok)
- `f1124.log` (~0 tok)
- `f1125.log` (~0 tok)
- `f1126.log` (~0 tok)
- `f1127.log` (~0 tok)
- `f1128.log` (~0 tok)
- `f1129.log` (~0 tok)
- `f113.log` (~0 tok)
- `f1130.log` (~0 tok)
- `f1131.log` (~0 tok)
- `f1132.log` (~0 tok)
- `f1133.log` (~0 tok)
- `f1134.log` (~0 tok)
- `f1135.log` (~0 tok)
- `f1136.log` (~0 tok)
- `f1137.log` (~0 tok)
- `f1138.log` (~0 tok)
- `f1139.log` (~0 tok)
- `f114.log` (~0 tok)
- `f1140.log` (~0 tok)
- `f1141.log` (~0 tok)
- `f1142.log` (~0 tok)
- `f1143.log` (~0 tok)
- `f1144.log` (~0 tok)
- `f1145.log` (~0 tok)
- `f1146.log` (~0 tok)
- `f1147.log` (~0 tok)
- `f1148.log` (~0 tok)
- `f1149.log` (~0 tok)
- `f115.log` (~0 tok)
- `f1150.log` (~0 tok)
- `f1151.log` (~0 tok)
- `f1152.log` (~0 tok)
- `f1153.log` (~0 tok)
- `f1154.log` (~0 tok)
- `f1155.log` (~0 tok)
- `f1156.log` (~0 tok)
- `f1157.log` (~0 tok)
- `f1158.log` (~0 tok)
- `f1159.log` (~0 tok)
- `f116.log` (~0 tok)
- `f1160.log` (~0 tok)
- `f1161.log` (~0 tok)
- `f1162.log` (~0 tok)
- `f1163.log` (~0 tok)
- `f1164.log` (~0 tok)
- `f1165.log` (~0 tok)
- `f1166.log` (~0 tok)
- `f1167.log` (~0 tok)
- `f1168.log` (~0 tok)
- `f1169.log` (~0 tok)
- `f117.log` (~0 tok)
- `f1170.log` (~0 tok)
- `f1171.log` (~0 tok)
- `f1172.log` (~0 tok)
- `f1173.log` (~0 tok)
- `f1174.log` (~0 tok)
- `f1175.log` (~0 tok)
- `f1176.log` (~0 tok)
- `f1177.log` (~0 tok)
- `f1178.log` (~0 tok)
- `f1179.log` (~0 tok)
- `f118.log` (~0 tok)
- `f1180.log` (~0 tok)
- `f1181.log` (~0 tok)
- `f1182.log` (~0 tok)
- `f1183.log` (~0 tok)
- `f1184.log` (~0 tok)
- `f1185.log` (~0 tok)
- `f1186.log` (~0 tok)
- `f1187.log` (~0 tok)
- `f1188.log` (~0 tok)
- `f1189.log` (~0 tok)
- `f119.log` (~0 tok)
- `f1190.log` (~0 tok)
- `f1191.log` (~0 tok)
- `f1192.log` (~0 tok)
- `f1193.log` (~0 tok)
- `f1194.log` (~0 tok)
- `f1195.log` (~0 tok)
- `f1196.log` (~0 tok)
- `f1197.log` (~0 tok)
- `f1198.log` (~0 tok)
- `f1199.log` (~0 tok)
- `f12.log` (~0 tok)
- `f120.log` (~0 tok)
- `f1200.log` (~0 tok)
- `f1201.log` (~0 tok)
- `f1202.log` (~0 tok)
- `f1203.log` (~0 tok)
- `f1204.log` (~0 tok)
- `f1205.log` (~0 tok)
- `f1206.log` (~0 tok)
- `f1207.log` (~0 tok)
- `f1208.log` (~0 tok)
- `f1209.log` (~0 tok)
- `f121.log` (~0 tok)
- `f1210.log` (~0 tok)
- `f1211.log` (~0 tok)
- `f1212.log` (~0 tok)
- `f1213.log` (~0 tok)
- `f1214.log` (~0 tok)
- `f1215.log` (~0 tok)
- `f1216.log` (~0 tok)
- `f1217.log` (~0 tok)
- `f1218.log` (~0 tok)
- `f1219.log` (~0 tok)
- `f122.log` (~0 tok)
- `f1220.log` (~0 tok)
- `f1221.log` (~0 tok)
- `f1222.log` (~0 tok)
- `f1223.log` (~0 tok)
- `f1224.log` (~0 tok)
- `f1225.log` (~0 tok)
- `f1226.log` (~0 tok)
- `f1227.log` (~0 tok)
- `f1228.log` (~0 tok)
- `f1229.log` (~0 tok)
- `f123.log` (~0 tok)
- `f1230.log` (~0 tok)
- `f1231.log` (~0 tok)
- `f1232.log` (~0 tok)
- `f1233.log` (~0 tok)
- `f1234.log` (~0 tok)
- `f1235.log` (~0 tok)
- `f1236.log` (~0 tok)
- `f1237.log` (~0 tok)
- `f1238.log` (~0 tok)
- `f1239.log` (~0 tok)
- `f124.log` (~0 tok)
- `f1240.log` (~0 tok)
- `f1241.log` (~0 tok)
- `f1242.log` (~0 tok)
- `f1243.log` (~0 tok)
- `f1244.log` (~0 tok)
- `f1245.log` (~0 tok)
- `f1246.log` (~0 tok)
- `f1247.log` (~0 tok)
- `f1248.log` (~0 tok)
- `f1249.log` (~0 tok)
- `f125.log` (~0 tok)
- `f1250.log` (~0 tok)
- `f1251.log` (~0 tok)
- `f1252.log` (~0 tok)
- `f1253.log` (~0 tok)
- `f1254.log` (~0 tok)
- `f1255.log` (~0 tok)
- `f1256.log` (~0 tok)
- `f1257.log` (~0 tok)
- `f1258.log` (~0 tok)
- `f1259.log` (~0 tok)
- `f126.log` (~0 tok)
- `f1260.log` (~0 tok)
- `f1261.log` (~0 tok)
- `f1262.log` (~0 tok)
- `f1263.log` (~0 tok)
- `f1264.log` (~0 tok)
- `f1265.log` (~0 tok)
- `f1266.log` (~0 tok)
- `f1267.log` (~0 tok)
- `f1268.log` (~0 tok)
- `f1269.log` (~0 tok)
- `f127.log` (~0 tok)
- `f1270.log` (~0 tok)
- `f1271.log` (~0 tok)
- `f1272.log` (~0 tok)
- `f1273.log` (~0 tok)
- `f1274.log` (~0 tok)
- `f1275.log` (~0 tok)
- `f1276.log` (~0 tok)
- `f1277.log` (~0 tok)
- `f1278.log` (~0 tok)
- `f1279.log` (~0 tok)
- `f128.log` (~0 tok)
- `f1280.log` (~0 tok)
- `f1281.log` (~0 tok)
- `f1282.log` (~0 tok)
- `f1283.log` (~0 tok)
- `f1284.log` (~0 tok)
- `f1285.log` (~0 tok)
- `f1286.log` (~0 tok)
- `f1287.log` (~0 tok)
- `f1288.log` (~0 tok)
- `f1289.log` (~0 tok)
