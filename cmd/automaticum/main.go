// Command automaticum is the command line of automaticum.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/mann1x/automaticum/internal/wait"
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
  suite import tbench2 -source D -commit C [<task>...]
                                import Terminal-Bench 2.0 tasks as the held-out suite tbench2, kept only if the oracle holds [--repeats 3]
  image build [<image>...]      build the lane images: base, then the language layers
  image ls                      the lane images
  sdk add <build>               install one SDK build's bundle and keep what it downloaded [--packs D for a local build]
  sdk ls                        the defined SDK builds and whether each can be used [--deep]
  sdk verify <build>            install a build again with no network from what was kept, and compare
  lane ls                       the lanes of this installation
  endpoint ls                   the endpoints of this installation
  status                        lanes, queue, what is running
  run start --arm A --task T    queue runs [--task T2 ...] [--suite S ...] [-n 5] [--lane L] [--dry-run] [--wall-s S] [--idle-s S] [--capture-requests] [--heldout] [--priority N]
  run ls                        queued, running and finished runs
  run show <run>                one run's record
  run tail <run>                what a run prints, as it arrives; a queued run is waited for until it starts
  run timeline [-requests] <run> the run turn by turn: calls and failures, guard trips, notices, forks, the end; -requests adds each model request's first byte, time and endpoint meta
  run stop <run>                take a queued run off the queue
  run abort <run>               end a run in flight
  run pause <run>               hold a run in flight between two model requests and free the endpoint of its model
  run resume <run>              let a held run go on
  run retry <run>               queue a run again that ended SKIPPED, ABORTED or INVALID [--dry-run]
  wait run <run> [<run>...]     until the run has finished, or --until "<condition>" holds [--any] [--timeout D]
  wait batch <batch>            until the batch has finished, or --until "pass>=5 or fail>=3"; --next: the next run to finish; --each: every run as it finishes
  wait cell|campaign <id>       until the cell or the campaign has finished [--until ...]
  wait lane <lane> | wait idle  until the lane, or every lane, has no run in flight and none queued
                                (exit 0 the condition holds, 4 timeout, 5 ended without it; automaticum wait run -h)
  lane drain <lane>             the run in flight finishes, then the lane starts nothing until resumed
  lane resume <lane>            the lane starts queued runs again (also lifts a pause or a quarantine)
  lane pause <lane>             drained, and its run in flight held with the endpoint freed [--keep-model]
  lane quarantine <lane>        drained and shown quarantined --reason R
  lane abort <lane>             abort the lane's run in flight
  lane show <lane>              a lane's configuration, state, queue and last runs [--recent N]
  lane reserve <lane>           only one batch or campaign starts there --for B --until T|8h
  lane unreserve <lane>         end a lane's reservation
  batch show <batch>            a batch's runs by state, whether it is paused, and what the canary said
  batch pause <batch>           start none of the batch's queued runs; hold its run in flight and free the endpoint [--keep-model]
  batch resume <batch>          let a paused batch go on
  batch stop <batch>            take the batch's queued runs off the queue; the run in flight finishes [--abort]
  batch retry <batch>           queue again every run of the batch that ended SKIPPED, ABORTED or INVALID [--dry-run]
  batch limits <batch>          the batch's own wall and idle limits for its queued runs, in place of the arm's and the task's [--wall-s S] [--idle-s S]
  run invalidate <run>          mark a finished run INVALID, --reason "..."
  run identify <run>            add where and on what a run ran to its record, marked as added later
  logs <run>                    a run's raw material [--events|--wire|--runner|--oracle]
  notices <run>... | --batch B  the SDK's notices with their sentences [--key K, as compare prints it]
  export conversations --out D  training runs' captured model requests, metadata and loop spans [--batch B|GLOB ...] [--run R ...] [--heldout]
  export cell <batch> --out D   a batch's cell files: runs.jsonl, basis.txt, automaticum_result.json [--format jsonl|csv]
  export campaign <name> --out D  every batch of a campaign as a cell, and campaign.json [--format jsonl|csv]
  export cohort <name> --out D  every cell of a cohort, cohort.json, and every run's numbers in runs.jsonl [--format jsonl|csv]
  export run <run>... --out D   a run's record and its stored artefacts [--kind K ...]
  doctor                        every gate, on every lane, now [--lane L] [--arm A] [--task T] [--all]
  compare <batch> [<batch> ...] the arms of some batches side by side: every statistic, no conclusion [--all] [--group arm.F|label.L|finding.D]
  report run|cell <id>          the same as run show, cell show
  report batch|ab <b> [<b>]     compare of one batch, or of two
  report regression <batch>...  the runs side by side by harness build (SDK, image, automaticum), one arm at a time [--all]
  report census [<batch>...]    tool calls by tool and model: failed, error classes, parse paths [--where ...]
  report struggle [<batch>...]  struggle signals (D126) against "did not pass": recall, false alarms [--where ...]
  report guards [<batch>...]    the guards' review queue: unconfirmed trips, missed turns [--kind ...] [--where ...]
  report skew <batch>...        lane skew: the same arm's runs by lane host, paired
  campaign start <name>         run campaigns/<name>.toml: its batches in order, with their stop conditions [--dry-run]
  campaign ls                   the campaigns
  cohort ls                     the cohorts: cells declared to differ in one variable (a campaign's [cohort], cell run --cohort)
  cohort show <name>            a cohort's cells side by side, the basis facts they differ in, and whether it is confounded
  campaign show <name>          where a campaign is, and each batch compared: the campaign's report [--all]
  campaign pause|resume|stop <name>
                                hold the campaign (its run in flight held, the endpoint freed), let it go on, or end it
  cell run --suite S[:subset] --arm A --out D --idempotency-key K
                                one cell, blocking, safe to re-run: the result file, runs.jsonl, exit 0/10/20/30/70 [-n 1] [--cohort N --declared-variable V]
  basis show <batch> ...        every fact that must be equal for two arms to be compared, per arm [--arm A]
  basis diff <b[/arm]> <b[/arm]>  exactly the facts in which two bases differ
  reanalyse                     derive metrics and findings again from what is stored [--run R | --batch B] [--imported]
  rescore <batch>               score a finished cell again with its tasks' current oracles, on its lanes; --out D writes its files again, the old result kept [--no-wait] [--dry-run]
  detect <file|run>             the detector library on a text file or on a stored run; --self-test runs its controls
  stub serve --script F         a scripted model server, for trying the harness without a GPU
  legacy import                 import the old harness's corpus (spec 100)
  store check                   verify every blob and every blob reference

Exit codes: 0 done; 1 done with a failing answer, or an error; 2 wrong arguments;
3 the daemon refused; 6 the daemon does not answer; 7 the daemon does not know
what was named; wait adds 4 (timeout) and 5 (ended without the condition), cell
run 10, 20, 30 and 70 (spec 065). --json prints JSON on every command.

Commands about runs talk to the daemon. The checkout with tasks/ and images/ is
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
	one := map[string]func(context.Context, []string) error{"status": status, "logs": logs, "reanalyse": reanalyse, "detect": detectCmd, "doctor": doctor, "compare": compare, "notices": notices, "rescore": rescore}
	if len(args) >= 1 && one[args[0]] != nil {
		fail(one[args[0]](ctx, args[1:]))
		return
	}
	if len(args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	cmds := map[string]func(context.Context, []string) error{
		"run start":            runStart,
		"run ls":               runLs,
		"run show":             runShow,
		"run tail":             runTail,
		"run timeline":         runTimeline,
		"run stop":             runPost("stop", "taken off the queue"),
		"run abort":            runPost("abort", "aborting"),
		"run pause":            runPost("pause", "will be held before its next model request, and the endpoint then freed of its model"),
		"run resume":           runPost("resume", "released; its next request loads the model again"),
		"run invalidate":       runPost("invalidate", "marked INVALID"),
		"run held":             runPost("held", "recorded as a held stream and decided again"),
		"run retry":            retry("run"),
		"lane drain":           laneControl("drain"),
		"lane resume":          laneControl("resume"),
		"lane pause":           laneControl("pause"),
		"lane quarantine":      laneControl("quarantine"),
		"lane abort":           laneControl("abort"),
		"lane reserve":         laneReserve(false),
		"lane unreserve":       laneReserve(true),
		"batch show":           batchShow,
		"batch start":          runStart,
		"cell run":             cellRun,
		"campaign start":       campaignStart,
		"campaign ls":          campaignLs,
		"campaign show":        campaignShow,
		"campaign pause":       campaignControl("pause"),
		"campaign resume":      campaignControl("resume"),
		"campaign stop":        campaignControl("stop"),
		"campaign abort":       campaignControl("abort"),
		"campaign retry":       campaignControl("retry"),
		"basis show":           basisShow,
		"basis diff":           basisDiff,
		"batch pause":          batchControl("pause"),
		"batch resume":         batchControl("resume"),
		"batch stop":           batchControl("stop"),
		"batch abort":          batchControl("abort"),
		"batch retry":          retry("batch"),
		"batch limits":         batchLimits,
		"export conversations": exportConversations,
		"export cell":          exportCell,
		"export campaign":      exportCampaign,
		"export cohort":        exportCohort,
		"cohort ls":            cohortLs,
		"cohort show":          cohortShow,
		"export run":           exportRunArtefacts,
		"report run":           reportRun,
		"report cell":          reportCell,
		"report batch":         reportBatch,
		"report ab":            reportAB,
		"report regression":    reportRegression,
		"report census":        reportCensus,
		"report struggle":      reportStruggle,
		"report guards":        reportGuards,
		"report skew":          reportSkew,
		"run identify":         runPost("identify", "where and on what it ran is now in the record, marked as added later"),
		"legacy import":        legacyImport,
		"store check":          storeCheck,
		"arm validate":         armValidate,
		"arm show":             armShow,
		"arm diff":             armDiff,
		"task ls":              taskLs,
		"task show":            taskShow,
		"task build":           taskBuild,
		"oracle verify":        oracleVerify,
		"suite import":         suiteImport,
		"lane ls":              laneLs,
		"lane show":            laneShow,
		"batch ls":             batchLs,
		"cell show":            cellShow,
		"cell ls":              cellLs,
		"arm ls":               armLs,
		"endpoint show":        endpointShow,
		"endpoint models":      endpointModels,
		"endpoint probe":       endpointProbe,
		"task verify":          taskVerify,
		"image build":          imageBuild,
		"image ls":             imageLs,
		"sdk add":              sdkAdd,
		"sdk ls":               sdkLs,
		"sdk verify":           sdkVerify,
		"stub serve":           stubServe,
		"endpoint ls":          endpointLs,
	}
	for _, kind := range wait.Kinds {
		cmds["wait "+kind] = waitCmd(kind)
	}
	cmd, ok := cmds[args[0]+" "+args[1]]
	if !ok {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	fail(cmd(ctx, args[2:]))
}

// The exit codes every command keeps (090 §1). `cell run` (0/10/20/30/70,
// 065 §2.1) and `wait` (4 timeout, 5 ended without the condition) add their
// own on top.
const (
	exitOK       = 0 // done
	exitFailed   = 1 // done, and the answer is a failure, or an error of its own
	exitUsage    = 2 // the arguments are wrong; nothing was done
	exitRefused  = 3 // the daemon refused the request
	exitDown     = 6 // the daemon does not answer
	exitNotFound = 7 // the daemon does not know what was named
)

// fail ends the command when err is one, with the code that says why.
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
	os.Exit(exitOf(err))
}

// exitOf is the code of an error that names none.
func exitOf(err error) int {
	var ae *apiErr
	switch {
	case errors.As(err, &ae) && ae.Status == 404:
		return exitNotFound
	case errors.As(err, &ae):
		return exitRefused
	case errors.Is(err, errDown):
		return exitDown
	case strings.HasPrefix(err.Error(), "usage:"):
		return exitUsage
	}
	return exitFailed
}
