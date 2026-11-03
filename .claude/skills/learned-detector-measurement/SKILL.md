---
name: learned-detector-measurement
description: Measure a detector or guard rule over every stored run on a lane host, read-only, to set or check its threshold and its margins on positive and negative runs
---
# Measuring a detector over every stored run on a lane host

Use this skill when a detector or guard needs a threshold, or when a rule changes and the threshold has to be measured again. Never invent the number (see the memory on thresholds from data). Everything here is read-only on the lane host.

## 1. List the runs and their groups

On the lane host, with `AUTOMATICUM_CONFIG` set and the CLI called by path:

```bash
$B/bin/automaticum run ls --json --limit 10000 > $W/zz-runs.json   # 10000 is the cap; more is refused
$B/bin/automaticum detect --all --finished --json > $W/zz-detect-all.json
```

`detect --json` is ONE JSON document (`header`, `runs`, `total`, `groups`, `end`), not JSON lines. Join the two files in python into a TSV with the columns run, arm, batch, sdk_release, verdict and cap_hit. `sdk_release` comes from `run ls`; `firings.cap_hit` comes from detect.

Define the groups before you look at the results:
- **positives:** for example, the gate runs that hit the output limit.
- **negatives:** base-model floor gates, pool runs on the shipped arm, the stub arms, and the earlier trained checkpoints the client calls healthy.

## 2. Build a throwaway measuring binary

```bash
mkdir -p cmd/zzmeasure && cat > cmd/zzmeasure/main.go <<'EOF'
// reads ev + wire paths from argv, uses ingest.ReadEvents, ingest.ReadWire,
// ev.WireAnswers(reqs), then the detect.* function under test; prints TSV
EOF
CGO_ENABLED=0 go build -o $SCRATCH/zzmeasure ./cmd/zzmeasure; rm -rf cmd/zzmeasure
```

- To call an unexported function, add a temporary `internal/detect/zz_export.go` that wraps it. Delete the file in the same command as the build.
- Check with `git status --short` that no `zz` file is left in the tree.
- Always call `ev.WireAnswers(reqs)`, so that the wire-only retry answers are measured too.
- Measure the cut-off (`Unfinished`) blocks as well.

## 3. Run it on the lane host, in the background, under a claim

```bash
scp -q $SCRATCH/zzmeasure <lanehost>:$W/zzmeasure
claude-hooks-res run <lanehost>/build:shared -r "<why, read-only>" -- ssh <lanehost> '
  W=...; mkdir -p $W/zzt; : > $W/zz-out.tsv
  while IFS=$(printf "\t") read -r run arm batch sdk verdict cap; do
    $B/bin/automaticum logs $run > $W/zzt/ev 2>/dev/null
    $B/bin/automaticum logs --kind wire $run > $W/zzt/wire 2>/dev/null || : > $W/zzt/wire
    $W/zzmeasure "$run" $W/zzt/ev $W/zzt/wire >> $W/zz-out.tsv
  done < $W/zz-list.tsv; rm -rf $W/zzt'
```

- Pass `run_in_background: true`. A pass over about 200 runs takes about 5 minutes, and one run's event stream can be 120 MB.
- Keep the scratch files on the host's persistent work disk, never /tmp.
- While it runs, check that it is alive: the output line count grows and `ps` shows the binary.

## 4. Report the margins both ways

- **Positives:** which ones it fires on, and how early (character offset, estimated tokens and seconds into the request).
- **Positives it misses:** why each one passes under every rule.
- **Negatives:** the closest any came to firing, on each condition of the rule.

Only keep a rule for an output channel (text, reasoning or arguments) where it stayed silent on every negative there.

Write the numbers into the code comment beside the constant and into the 070 spec. Send them to the client before choosing anything they did not ask for.

## 5. Clean up

Delete `$W/zz-*`, the binaries on the host and the local scratch copies. The scratch directory may be under the session's /tmp scratchpad only while the files are under about 1 MB; otherwise use persistent disk.