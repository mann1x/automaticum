package main

import (
	"archive/tar"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mann1x/automaticum/internal/daemon"
	"github.com/mann1x/automaticum/internal/docker"
)

// buildContext writes what an image build reads: the runner and the image
// definitions. Tests and installed modules are left out.
func buildContext(root string, w io.Writer) error {
	tw := tar.NewWriter(w)
	for _, top := range []string{"runner", "images"} {
		err := filepath.WalkDir(filepath.Join(root, top), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if n := d.Name(); n == "node_modules" || n == "test" {
					return filepath.SkipDir
				}
				return nil
			}
			if !d.Type().IsRegular() {
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(root, path)
			if err := tw.WriteHeader(&tar.Header{Name: filepath.ToSlash(rel), Mode: 0o644, Size: info.Size(), Typeflag: tar.TypeReg}); err != nil {
				return err
			}
			f, err := os.Open(path)
			if err != nil {
				return err
			}
			defer f.Close()
			_, err = io.CopyN(tw, f, info.Size())
			return err
		})
		if err != nil {
			return err
		}
	}
	return tw.Close()
}

// imageNames lists what can be built: the base, then the layers.
func imageNames(root string) ([]string, error) {
	out := []string{"base"}
	entries, err := os.ReadDir(filepath.Join(root, "images", "layers"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	for _, e := range entries {
		if e.IsDir() {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out[1:])
	return out, nil
}

func imageBuild(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("image build", flag.ExitOnError)
	p := addPlace(fs)
	dry := fs.Bool("dry-run", false, "say which images would be built, from which Dockerfile; build nothing")
	rest := parse(fs, args)
	l, err := p.local()
	if err != nil {
		return err
	}
	root := p.root()
	all, err := imageNames(root)
	if err != nil {
		return err
	}
	names := rest
	if len(names) == 0 {
		names = all
	}
	engine := docker.New(l.Daemon.DockerSocket)
	type built struct {
		Image string `json:"image"`
		Tag   string `json:"tag"`
		ID    string `json:"id"`
	}
	var done []built
	for _, name := range names {
		file := "images/base/Dockerfile"
		opts := docker.BuildOptions{Tag: daemon.LaneImage(nil)}
		if name != "base" {
			file = "images/layers/" + name + "/Dockerfile"
			opts = docker.BuildOptions{Tag: daemon.LaneImage([]string{name})}
		}
		if _, err := os.Stat(filepath.Join(root, file)); err != nil {
			return fmt.Errorf("there is no image %q; there are: %s", name, strings.Join(all, ", "))
		}
		opts.Dockerfile = file
		if *dry {
			done = append(done, built{Image: name, Tag: opts.Tag, ID: "(dry run) " + file})
			continue
		}
		pr, pw := io.Pipe()
		go func() { pw.CloseWithError(buildContext(root, pw)) }()
		fmt.Fprintf(os.Stderr, "building %s\n", opts.Tag)
		err := engine.ImageBuild(ctx, pr, opts, os.Stderr)
		pr.Close()
		if err != nil {
			return fmt.Errorf("image %s: %w", name, err)
		}
		im, err := engine.ImageInspect(ctx, opts.Tag)
		if err != nil {
			return err
		}
		done = append(done, built{name, opts.Tag, im.ID})
		if !*p.json {
			fmt.Printf("%s  %s\n", opts.Tag, im.ID)
		}
	}
	if *p.json {
		return printJSON(done)
	}
	return nil
}

func imageLs(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("image ls", flag.ExitOnError)
	p := addPlace(fs)
	parse(fs, args)
	l, err := p.local()
	if err != nil {
		return err
	}
	names, err := imageNames(p.root())
	if err != nil {
		return err
	}
	engine := docker.New(l.Daemon.DockerSocket)
	type row struct {
		Image   string `json:"image"`
		Tag     string `json:"tag"`
		ID      string `json:"id,omitempty"`
		Created string `json:"created,omitempty"`
		Built   bool   `json:"built"`
	}
	var rows []row
	w := table()
	fmt.Fprintln(w, "IMAGE\tTAG\tID\tCREATED")
	for _, name := range names {
		tag := daemon.LaneImage(nil)
		if name != "base" {
			tag = daemon.LaneImage([]string{name})
		}
		im, err := engine.ImageInspect(ctx, tag)
		if err != nil {
			rows = append(rows, row{Image: name, Tag: tag})
			fmt.Fprintf(w, "%s\t%s\tnot built\t\n", name, tag)
			continue
		}
		rows = append(rows, row{name, tag, im.ID, im.Created, true})
		id := strings.TrimPrefix(im.ID, "sha256:")
		if len(id) > 12 {
			id = id[:12]
		}
		created := im.Created
		if len(created) > 19 {
			created = created[:19]
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", name, tag, id, created)
	}
	if *p.json {
		return printJSON(rows)
	}
	return w.Flush()
}
