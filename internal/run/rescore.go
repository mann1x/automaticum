package run

import (
	"archive/tar"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"

	"github.com/mann1x/automaticum/internal/store"
)

// Rescore decides a run's verdict again from a new answer of its primary
// oracle (D121, 065 §4): the facts the run recorded (the limit that fired,
// a guard stop, a server fault the agent ended with), with the new answer
// in place of the old. Only a run whose verdict the oracle gave is
// rescored: PASS, FAIL and TIMEOUT; ok is false for any other.
func Rescore(r *store.Run, passed bool) (v store.Verdict, reason string, ok bool) {
	switch r.Verdict {
	case store.Pass, store.Fail, store.Timeout:
	default:
		return "", "", false
	}
	var p struct {
		Limits struct {
			Wall    limit  `json:"wall"`
			Idle    limit  `json:"idle"`
			Fired   string `json:"fired"`
			FiredBy string `json:"fired_by"`
		} `json:"limits"`
		Runner *End `json:"runner"`
	}
	json.Unmarshal(r.Provenance, &p)
	f := Facts{OracleRan: true, OraclePassed: passed}
	switch p.Limits.Fired {
	case "wall":
		f.Limit, f.LimitOwner, f.LimitBy = "wall", p.Limits.Wall.Owner, p.Limits.FiredBy
	case "idle":
		f.Limit, f.LimitOwner, f.LimitBy = "idle", p.Limits.Idle.Owner, p.Limits.FiredBy
	}
	if end := p.Runner; end != nil {
		if end.Outcome == OutcomeGuardStop {
			f.GuardStop = end.Reason
		}
		if end.AgentEnd == "error" {
			if kind := Fault(end.AgentError); kind != "" {
				f.Infra = kind + ": " + end.AgentError
			}
		}
	}
	v, reason = Decide(f)
	return v, reason, true
}

// Untar writes a tree stored by tarTree into dir, which must exist. Every
// name stays inside dir, links included.
func Untar(r io.Reader, dir string) error {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()
	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		name := path.Clean(hdr.Name)
		if name == "." || path.IsAbs(name) || name == ".." || strings.HasPrefix(name, "../") {
			return fmt.Errorf("the stored tree names %q, outside it", hdr.Name)
		}
		mode := os.FileMode(hdr.Mode).Perm()
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := root.MkdirAll(name, mode|0o700); err != nil {
				return err
			}
		case tar.TypeSymlink:
			if err := root.Symlink(hdr.Linkname, name); err != nil {
				return err
			}
		case tar.TypeReg:
			f, err := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
			if err != nil {
				return err
			}
			_, err = io.Copy(f, tr)
			if cerr := f.Close(); err == nil {
				err = cerr
			}
			if err != nil {
				return err
			}
		}
	}
}
