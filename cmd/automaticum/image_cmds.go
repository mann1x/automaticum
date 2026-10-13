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

	"github.com/mann1x/automaticum/internal/config"
	"github.com/mann1x/automaticum/internal/daemon"
	"github.com/mann1x/automaticum/internal/docker"
	"github.com/mann1x/automaticum/internal/run"
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
	release := fs.String("sdk-release", "", "the SDK release the base image must carry; that of arms/base.toml when not given")
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
	if *release == "" {
		base, err := config.LoadArm(p.arms(), "base")
		if err != nil {
			return err
		}
		*release = base.Harness.SDKRelease
	}
	engine := docker.New(l.Daemon.DockerSocket)
	for _, name := range names {
		file := "images/base/Dockerfile"
		opts := docker.BuildOptions{Tag: daemon.LaneImage(nil), Args: map[string]string{"SDK_RELEASE": *release}}
		if name != "base" {
			file = "images/layers/" + name + "/Dockerfile"
			opts = docker.BuildOptions{Tag: daemon.LaneImage([]string{name})}
		}
		if _, err := os.Stat(filepath.Join(root, file)); err != nil {
			return fmt.Errorf("there is no image %q; there are: %s", name, strings.Join(all, ", "))
		}
		opts.Dockerfile = file
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
		fmt.Printf("%s  %s  SDK release %s\n", opts.Tag, im.ID, im.Config.Labels[run.SDKReleaseLabel])
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
	w := table()
	fmt.Fprintln(w, "IMAGE\tTAG\tSDK RELEASE\tID\tCREATED")
	for _, name := range names {
		tag := daemon.LaneImage(nil)
		if name != "base" {
			tag = daemon.LaneImage([]string{name})
		}
		im, err := engine.ImageInspect(ctx, tag)
		if err != nil {
			fmt.Fprintf(w, "%s\t%s\tnot built\t\t\n", name, tag)
			continue
		}
		id := strings.TrimPrefix(im.ID, "sha256:")
		if len(id) > 12 {
			id = id[:12]
		}
		created := im.Created
		if len(created) > 19 {
			created = created[:19]
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", name, tag, im.Config.Labels[run.SDKReleaseLabel], id, created)
	}
	return w.Flush()
}
