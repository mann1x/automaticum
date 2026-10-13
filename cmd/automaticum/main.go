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
  lane ls                       the lanes of this installation
  endpoint ls                   the endpoints of this installation
  legacy import                 import the old harness's corpus (spec 100)
  store check                   verify every blob and every blob reference

The checkout with arms/ and tasks/ is --repo, AUTOMATICUM_REPO, the "repo" of
the configuration, or the current directory. The configuration directory is
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
	if len(args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	cmds := map[string]func(context.Context, []string) error{
		"legacy import": legacyImport,
		"store check":   storeCheck,
		"arm validate":  armValidate,
		"arm show":      armShow,
		"arm diff":      armDiff,
		"task ls":       taskLs,
		"task show":     taskShow,
		"task build":    taskBuild,
		"oracle verify": oracleVerify,
		"lane ls":       laneLs,
		"endpoint ls":   endpointLs,
	}
	cmd, ok := cmds[args[0]+" "+args[1]]
	if !ok {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	if err := cmd(ctx, args[2:]); err != nil {
		if err != errFailed {
			fmt.Fprintln(os.Stderr, "automaticum:", err)
		}
		os.Exit(1)
	}
}
