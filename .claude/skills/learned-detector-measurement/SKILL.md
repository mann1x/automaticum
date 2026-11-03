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

## Measuring while a client's batch is running

Never start a measurement while a client's batch runs on the lane: reading hundreds of runs' blobs competes with the run for disk and CPU. Put the wait and the measurement in one background command, so the measurement starts only when the batch has ended:

```bash
ssh <host> '$B/bin/automaticum wait batch <batch> --timeout 2h | tail -3' && \
  claude-hooks-res run <host>/build:shared -r "read-only <rule> measurement over stored runs" -- \
  ssh <host> 'bash $B/work/zz-remote.sh'
```

Run it with `run_in_background: true`. While you wait, carry on with the code and the tests.

## The per-run loop on the host

Copy the throwaway binary and a driver script to `$B/work` (persistent disk). In the script:
- write `run ls --json --limit 10000` to a TSV of run, arm, batch, sdk_release and verdict, for finished runs only;
- for each run, write `automaticum logs <run>` and `automaticum logs --kind wire <run>` to one reused scratch pair (`zzt/ev`, `zzt/wire`; `|| : > zzt/wire` when a run has no wire capture);
- call the binary with the run, its `sdk_release` (for `detect.RulesFor`) and the two paths, and append its TSV row after the arm, batch and verdict;
- `rm -rf zzt` at the end, and print `MEASURE-DONE <rows>` so the background output shows it finished.

Then summarise by arm: `awk -F'\t' '{a[$1]++; if($6>0)f[$1]++} END{for(k in a) print a[k], f[k]+0, k}'`. Count the `err` rows separately.

## Reading a firing on a run you thought was healthy

- First check the run's stored analysis version. Runs analysed before analysis version 17 have no wire-only answers, so a real loop up to the output limit may never have appeared in their stored findings.
- Look for the text in the wire capture (`logs --kind wire`), not `logs --render`: the render does not hold wire-only answers. The wire is JSON-escaped several times over, so search for a phrase without newlines or quotes, and in python print `repr()` of the slice around `t.find(phrase)`.
- If the output really is a loop written to the limit, it is a positive. Report it in the spec's margins paragraph as such, with turn, offset and length. Do not report it as a false positive.
- Guard every shell arithmetic on a grep result (`L=$(grep -n ... | head -1 | cut -d: -f1)`): an empty `L` turns `sed -n "$((L-6)),..."` into `sed: invalid option -- '6'`.

## Cleanup

Delete `$B/work/zz*` (runs, list, out, binary, script) on the host once you have written the numbers into 070 and the code comment. Locally, check `git status --short | grep zz` to make sure nothing is left.