// Command automaticum is the command line of automaticum.
package main

import (
	"context"
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
  image ls                      the lane images and the SDK release each carries
  lane ls                       the lanes of this installation
  endpoint ls                   the endpoints of this installation
  status                        lanes, queue, what is running
  run start --arm A --task T    queue runs [-n 5] [--lane L] [--dry-run]
  run ls                        queued, running and finished runs
  run show <run>                one run's record
  run tail <run>                what a run in flight prints, as it arrives
  run stop <run>                take a queued run off the queue
  run abort <run>               end a run in flight
  run invalidate <run>          mark a finished run INVALID, --reason "..."
  logs <run>                    a run's raw material [--events|--wire|--runner|--oracle]
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
	one := map[string]func(context.Context, []string) error{"status": status, "logs": logs, "reanalyse": reanalyse, "detect": detectCmd}
	if len(args) >= 1 && one[args[0]] != nil {
		fail(one[args[0]](ctx, args[1:]))
		return
	}
	if len(args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	cmds := map[string]func(context.Context, []string) error{
		"run start":      runStart,
		"run ls":         runLs,
		"run show":       runShow,
		"run tail":       runTail,
		"run stop":       runPost("stop", "taken off the queue"),
		"run abort":      runPost("abort", "aborting"),
		"run invalidate": runPost("invalidate", "marked INVALID"),
		"legacy import":  legacyImport,
		"store check":    storeCheck,
		"arm validate":   armValidate,
		"arm show":       armShow,
		"arm diff":       armDiff,
		"task ls":        taskLs,
		"task show":      taskShow,
		"task build":     taskBuild,
		"oracle verify":  oracleVerify,
		"lane ls":        laneLs,
		"image build":    imageBuild,
		"image ls":       imageLs,
		"stub serve":     stubServe,
		"endpoint ls":    endpointLs,
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
	if err != errFailed {
		fmt.Fprintln(os.Stderr, "automaticum:", err)
	}
	os.Exit(1)
}
