package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mann1x/automaticum/internal/config"
	"github.com/mann1x/automaticum/internal/docker"
	"github.com/mann1x/automaticum/internal/human"
	"github.com/mann1x/automaticum/internal/sdk"
)

// SDKEnv is where the SDK builds of an installation are: definitions in the
// checkout, bundles under the data root, and what each install downloaded
// in the archive tier when there is one.
func SDKEnv(l *config.Local, repo string) *sdk.Env {
	archive := filepath.Join(l.Daemon.DataRoot, "sdk-packages")
	if l.Daemon.ArchiveRoot != "" {
		archive = filepath.Join(l.Daemon.ArchiveRoot, "sdk-packages")
	}
	return &sdk.Env{Exec: docker.New(l.Daemon.DockerSocket), Defs: filepath.Join(repo, "runner", "sdk"),
		Root: filepath.Join(l.Daemon.DataRoot, "sdk"), Archive: archive}
}

func sdkEnv(p place) (*sdk.Env, error) {
	l, err := p.local()
	if err != nil {
		return nil, err
	}
	return SDKEnv(l, p.root()), nil
}

// sdkAdd installs one build's bundle.
func sdkAdd(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("sdk add", flag.ExitOnError)
	p := addPlace(fs)
	packs := fs.String("packs", "", "for a local build: the directory of its packed packages")
	dry := fs.Bool("dry-run", false, "say what would be installed and whether its hash is pinned; install nothing")
	rest := parse(fs, args)
	if len(rest) != 1 {
		return errors.New("usage: automaticum sdk add <build> [--packs <dir>]")
	}
	env, err := sdkEnv(p)
	if err != nil {
		return err
	}
	before, err := env.Def(rest[0])
	if err != nil {
		return err
	}
	if *dry {
		pinned := "not pinned yet: the first install writes its hash into runner/sdk/" + rest[0] + "/sdk.toml"
		if before.BundleSHA256 != "" {
			pinned = "pinned to bundle " + before.BundleSHA256
		}
		if *p.json {
			return printJSON(map[string]any{"build": rest[0], "definition": before, "dry_run": true})
		}
		fmt.Printf("would install SDK build %s; %s\n", rest[0], pinned)
		return nil
	}
	rec, err := env.Add(ctx, rest[0], *packs, os.Stderr)
	if err != nil {
		return err
	}
	if *p.json {
		return printJSON(rec)
	}
	fmt.Printf("SDK build %s: package version %s, %s files, bundle %s\n", rec.Name, rec.Version, human.Count(int64(rec.Files)), rec.BundleSHA256)
	fmt.Printf("what it downloaded is kept: %s, %.12s\n", human.Bytes(rec.PackagesBytes), rec.PackagesSHA256)
	if before.BundleSHA256 == "" {
		fmt.Printf("this was its first install: the hash is now in runner/sdk/%s/sdk.toml. Commit that file; no run uses the build before it is pinned there.\n", rec.Name)
	} else {
		fmt.Println("the bundle is the one its definition pins")
	}
	return nil
}

// sdkLs lists the defined builds and whether each can be used.
func sdkLs(_ context.Context, args []string) error {
	fs := flag.NewFlagSet("sdk ls", flag.ExitOnError)
	p := addPlace(fs)
	deep := fs.Bool("deep", false, "hash every bundle's files again")
	parse(fs, args)
	env, err := sdkEnv(p)
	if err != nil {
		return err
	}
	names, err := env.List()
	if err != nil {
		return err
	}
	type row struct {
		Build   string `json:"build"`
		Version string `json:"version,omitempty"`
		Source  string `json:"source,omitempty"`
		Bundle  string `json:"bundle_sha256,omitempty"`
		State   string `json:"state"`
		Usable  bool   `json:"usable"`
	}
	var rows []row
	w := table()
	fmt.Fprintln(w, "BUILD\tVERSION\tSOURCE\tBUNDLE\tSTATE")
	for _, name := range names {
		d, rec, err := env.Check(name, *deep)
		version, source, bundle, state := "", "", "", "usable"
		if d != nil {
			version, source = d.Version, d.Source
			if d.Commit != "" {
				source += " " + d.Commit
			}
			if len(d.BundleSHA256) >= 12 {
				bundle = d.BundleSHA256[:12]
			}
		}
		switch {
		case errors.Is(err, sdk.ErrNotProvisioned):
			state = "not installed"
		case err != nil:
			state = err.Error()
		case rec != nil && *deep:
			state = "usable, files checked"
		}
		full := ""
		if d != nil {
			full = d.BundleSHA256
		}
		rows = append(rows, row{name, version, source, full, state, err == nil})
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", name, version, source, bundle, state)
	}
	if *p.json {
		return printJSON(rows)
	}
	return w.Flush()
}

// sdkVerify proves a build can be made again: it is installed anew, with no
// network, from the archived packages, and must give the pinned bundle.
func sdkVerify(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("sdk verify", flag.ExitOnError)
	p := addPlace(fs)
	rest := parse(fs, args)
	if len(rest) != 1 {
		return errors.New("usage: automaticum sdk verify <build>")
	}
	env, err := sdkEnv(p)
	if err != nil {
		return err
	}
	d, _, err := env.Check(rest[0], true)
	if err != nil {
		return err
	}
	sum, err := env.Rebuild(ctx, rest[0], os.Stderr)
	if err != nil {
		return err
	}
	if *p.json {
		if err := printJSON(map[string]any{"build": rest[0], "reproduced": sum == d.BundleSHA256, "bundle_sha256": sum, "pinned": d.BundleSHA256}); err != nil {
			return err
		}
		if sum != d.BundleSHA256 {
			return errFailed
		}
		return nil
	}
	if sum != d.BundleSHA256 {
		return fmt.Errorf("SDK build %s is NOT reproduced: the archived packages install as %s, pinned %s", rest[0], sum, d.BundleSHA256)
	}
	fmt.Printf("SDK build %s is reproduced: installed again with no network from the archived packages, bundle %s as pinned; the installed files hash to the same\n", rest[0], sum)
	return nil
}
