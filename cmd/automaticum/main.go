// Command automaticum is the command line of automaticum.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

const usage = `usage: automaticum <noun> <verb> [flags] [arguments]

  version                       print the version
  arm validate [<arm>...]       check arms; all of them when none is named
  arm show <arm>                the resolved arm, with the origin of every value
  arm diff <arm> <arm>          the fields in which two arms differ
  task ls                       the tasks
  task show <task>              one task and the hash of its files
  task build <task>             build the task's artefacts and check their hashes
  oracle verify <task>          prove the primary oracle against the fixtures
  image build [<image>...]      build the lane images: base, then the language layers
  image ls                      the lane images
  sdk add <build>               install one SDK build's bundle and keep what it downloaded [--packs D for a local build]
  sdk ls                        the defined SDK builds and whether each can be used [--deep]
  sdk verify <build>            install a build again with no network from what was kept, and compare
  lane ls                       the lanes of this installation
  endpoint ls                   the endpoints of this installation
  status                        lanes, queue, what is running
  run start --arm A --task T    queue runs [--task T2 ...] [--suite S ...] [-n 5] [--lane L] [--dry-run] [--wall-s S] [--idle-s S]
  run ls                        queued, running and finished runs
  run show <run>                one run's record
  run tail <run>                what a run in flight prints, as it arrives
  run stop <run>                take a queued run off the queue
  run abort <run>               end a run in flight
  run pause <run>               hold a run in flight between two model requests and free the endpoint of its model
  run resume <run>              let a held run go on
  run retry <run>               queue a run again that ended SKIPPED, ABORTED or INVALID [--dry-run]
  lane drain <lane>             the run in flight finishes, then the lane starts nothing until resumed
  lane resume <lane>            the lane starts queued runs again
  batch show <batch>            a batch's runs by state, whether it is paused, and what the canary said
  batch pause <batch>           start none of the batch's queued runs; hold its run in flight and free the endpoint [--keep-model]
  batch resume <batch>          let a paused batch go on
  batch stop <batch>            take the batch's queued runs off the queue; the run in flight finishes [--abort]
  batch retry <batch>           queue again every run of the batch that ended SKIPPED, ABORTED or INVALID [--dry-run]
  batch limits <batch>          the batch's own wall and idle limits for its queued runs, in place of the arm's and the task's [--wall-s S] [--idle-s S]
  run invalidate <run>          mark a finished run INVALID, --reason "..."
  run identify <run>            add where and on what a run ran to its record, marked as added later
  logs <run>                    a run's raw material [--events|--wire|--runner|--oracle]
  doctor                        every gate, on every lane, now [--lane L] [--arm A] [--task T] [--all]
  compare <batch> [<batch> ...] the arms of some batches side by side: every statistic, no conclusion [--all]
  campaign start <name>         run campaigns/<name>.toml: its batches in order, with their stop conditions [--dry-run]
  campaign ls                   the campaigns
  campaign show <name>          where a campaign is, and each batch compared: the campaign's report [--all]
  campaign pause|resume|stop <name>
                                hold the campaign (its run in flight held, the endpoint freed), let it go on, or end it
  cell run --suite S[:subset] --arm A --out D --idempotency-key K
                                one cell, blocking, safe to re-run: the result file, runs.jsonl, exit 0/10/20/30/70 [-n 1]
  basis show <batch> ...        every fact that must be equal for two arms to be compared, per arm [--arm A]
  basis diff <b[/arm]> <b[/arm]>  exactly the facts in which two bases differ
  reanalyse                     derive metrics and findings again from what is stored [--run R | --batch B] [--imported]
  detect <file|run>             the detector library on a text file or on a stored run; --self-test runs its controls
  stub serve --script F         a scripted model server, for trying the harness without a GPU
  legacy import                 import the old harness's corpus (spec 100)
  store check                   verify every blob and every blob reference

Commands about runs talk to the daemon. The checkout with arms/ and tasks/ is
--repo, AUTOMATICUM_REPO, the "repo" of the configuration, or the current directory. The configuration directory is
--config or AUTOMATICUM_CONFIG. Store paths come from flags or from
AUTOMATICUM_DATA, AUTOMATICUM_ARCHIVE, AUTOMATICUM_LEGACY_ROOT and
AUTOMATICUM_LEGACY_STATE_ARCHIVE.
`

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	args := os.Args[1:]
	if len(args) == 1 && args[0] == "version" {
		fmt.Println(version)
		return
	}
	// Some commands are one word.
	one := map[string]func(context.Context, []string) error{"status": status, "logs": logs, "reanalyse": reanalyse, "detect": detectCmd, "doctor": doctor, "compare": compare}
	if len(args) >= 1 && one[args[0]] != nil {
		fail(one[args[0]](ctx, args[1:]))
		return
	}
	if len(args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	cmds := map[string]func(context.Context, []string) error{
		"run start":       runStart,
		"run ls":          runLs,
		"run show":        runShow,
		"run tail":        runTail,
		"run stop":        runPost("stop", "taken off the queue"),
		"run abort":       runPost("abort", "aborting"),
		"run pause":       runPost("pause", "will be held before its next model request, and the endpoint then freed of its model"),
		"run resume":      runPost("resume", "released; its next request loads the model again"),
		"run invalidate":  runPost("invalidate", "marked INVALID"),
		"run retry":       retry("run"),
		"lane drain":      laneControl(true),
		"lane resume":     laneControl(false),
		"batch show":      batchShow,
		"cell run":        cellRun,
		"campaign start":  campaignStart,
		"campaign ls":     campaignLs,
		"campaign show":   campaignShow,
		"campaign pause":  campaignControl("pause"),
		"campaign resume": campaignControl("resume"),
		"campaign stop":   campaignControl("stop"),
		"basis show":      basisShow,
		"basis diff":      basisDiff,
		"batch pause":     batchControl("pause"),
		"batch resume":    batchControl("resume"),
		"batch stop":      batchControl("stop"),
		"batch retry":     retry("batch"),
		"batch limits":    batchLimits,
		"run identify":    runPost("identify", "where and on what it ran is now in the record, marked as added later"),
		"legacy import":   legacyImport,
		"store check":     storeCheck,
		"arm validate":    armValidate,
		"arm show":        armShow,
		"arm diff":        armDiff,
		"task ls":         taskLs,
		"task show":       taskShow,
		"task build":      taskBuild,
		"oracle verify":   oracleVerify,
		"lane ls":         laneLs,
		"image build":     imageBuild,
		"image ls":        imageLs,
		"sdk add":         sdkAdd,
		"sdk ls":          sdkLs,
		"sdk verify":      sdkVerify,
		"stub serve":      stubServe,
		"endpoint ls":     endpointLs,
	}
	cmd, ok := cmds[args[0]+" "+args[1]]
	if !ok {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	fail(cmd(ctx, args[2:]))
}

// fail ends the command when err is one.
func fail(err error) {
	if err == nil {
		return
	}
	var code exitCode
	if errors.As(err, &code) {
		os.Exit(int(code))
	}
	if err != errFailed {
		fmt.Fprintln(os.Stderr, "automaticum:", err)
	}
	os.Exit(1)
}
