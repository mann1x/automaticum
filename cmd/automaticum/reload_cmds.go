package main

import (
	"context"
	"flag"
	"fmt"
	"strings"
	"time"

	"github.com/mann1x/automaticum/internal/daemon"
	"github.com/mann1x/automaticum/internal/human"
)

// configReload asks the daemon to read its configuration again and put it
// in force without stopping anything, as SIGHUP does; --dry-run says what
// would change. A configuration that does not load changes nothing and the
// command fails with why.
func configReload(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("config reload", flag.ExitOnError)
	_, dial := addClient(fs)
	m := addMutation(fs)
	if rest := parse(fs, args); len(rest) > 0 {
		return fmt.Errorf("usage: automaticum config reload [--dry-run] [--json]")
	}
	c, err := dial()
	if err != nil {
		return err
	}
	var res daemon.ReloadResult
	if err := c.do(ctx, "POST", m.on(c, "/v1/config/reload"), nil, &res); err != nil {
		return fmt.Errorf("the configuration was not reloaded; the one in force stays: %w", err)
	}
	if c.json {
		return printJSON(res)
	}
	printReload(m.dryNote(), res)
	return nil
}

func printReload(note string, res daemon.ReloadResult) {
	ch := res.Change
	if ch.Empty() {
		fmt.Println(note + "configuration read again: nothing changed")
	} else {
		fmt.Println(note + "configuration reloaded")
	}
	list := func(label string, v []string) {
		if len(v) > 0 {
			fmt.Printf("  %-26s %s\n", label+":", strings.Join(v, ", "))
		}
	}
	list("added", ch.Added)
	list("removed", ch.Removed)
	list("changed", ch.Changed)
	list("settings now in force", ch.Settings)
	list("only from the next start", ch.Restart)
	list("retiring after their run", res.Retiring)
	for _, w := range res.Waiting {
		fmt.Printf("  waiting:                   %s\n", w)
	}
	for _, u := range res.Unforwarded {
		fmt.Printf("  not forwarding:            %s (tried again every 30s)\n", u)
	}
}

// configShow prints the configuration in force and its last reload.
func configShow(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("config show", flag.ExitOnError)
	_, dial := addClient(fs)
	if rest := parse(fs, args); len(rest) > 0 {
		return fmt.Errorf("usage: automaticum config show [--json]")
	}
	c, err := dial()
	if err != nil {
		return err
	}
	var st daemon.ConfigState
	if err := c.do(ctx, "GET", "/v1/config", nil, &st); err != nil {
		return err
	}
	if c.json {
		return printJSON(st)
	}
	watch := "off: SIGHUP or `automaticum config reload` only"
	if st.Watching {
		watch = "on: a change in the directory is put in force on its own"
	}
	fmt.Printf("directory   %s\nloaded      %s (%s ago), %d reloads since the start\nwatching    %s\nlanes       %s\nendpoints   %s\n",
		st.Dir, human.Time(st.Loaded), human.Duration(time.Since(st.Loaded)), st.Reloads, watch,
		strings.Join(st.Lanes, " "), strings.Join(st.Endpoints, " "))
	for _, f := range st.Forwards {
		state := "listening"
		if f.Error != "" {
			state = "NOT listening: " + f.Error
		}
		fmt.Printf("forward     %s -> %s for %s: %s\n", f.Listen, f.Target, strings.Join(f.Endpoints, ", "), state)
	}
	if len(st.Restart) > 0 {
		fmt.Printf("restart     changed but held from the next start only: %s\n", strings.Join(st.Restart, ", "))
	}
	if l := st.Last; l != nil {
		if l.Error != "" {
			fmt.Printf("last reload %s (%s) REFUSED, the configuration in force stays:\n  %s\n", human.Time(l.At), l.Source, strings.ReplaceAll(l.Error, "\n", "\n  "))
		} else {
			fmt.Printf("last reload %s (%s)\n", human.Time(l.At), l.Source)
			printReload("  ", *l)
		}
	}
	return nil
}
