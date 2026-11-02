package ingest

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/mann1x/automaticum/internal/store"
)

// OutsideWorkspace is the finding that the agent's container wrote in its
// own filesystem, outside the workspace and the harness's mounts (070 §4,
// integrity): every path it added, changed or deleted there, by directory.
// It judges nothing yet: which of them the runner and the SDK write on
// every run is to be seen from the runs.
const OutsideWorkspace = "outside_workspace"

// changeDepth is how many path components a change is grouped under.
const changeDepth = 3

// integrity reads the container's changes the run recorded:
// integrity.outside_workspace and its .added, .changed and .deleted, and
// the finding when there was any. A directory reported changed only
// because something under it changed is not counted.
func integrity(r *store.Run, m func(string, float64), res *Result) {
	var p struct {
		Container struct {
			Changes *struct {
				Count int `json:"count"`
				Paths []struct {
					Path string `json:"path"`
					Kind int    `json:"kind"`
				} `json:"paths"`
				Error string `json:"error"`
			} `json:"changes"`
		} `json:"container"`
	}
	json.Unmarshal(r.Provenance, &p)
	ch := p.Container.Changes
	if ch == nil || ch.Error != "" {
		return
	}
	parent := map[string]bool{}
	for _, c := range ch.Paths {
		for d := c.Path; ; {
			i := strings.LastIndex(d, "/")
			if i <= 0 {
				break
			}
			d = d[:i]
			parent[d] = true
		}
	}
	kinds := map[int]int{}
	byDir := map[string]int{}
	for _, c := range ch.Paths {
		if c.Kind == 0 && parent[c.Path] {
			continue
		}
		kinds[c.Kind]++
		parts := strings.Split(strings.TrimPrefix(c.Path, "/"), "/")
		if len(parts) > changeDepth {
			parts = parts[:changeDepth]
		}
		byDir["/"+strings.Join(parts, "/")]++
	}
	n := kinds[0] + kinds[1] + kinds[2]
	m("integrity.outside_workspace", float64(n))
	m("integrity.outside_workspace.changed", float64(kinds[0]))
	m("integrity.outside_workspace.added", float64(kinds[1]))
	m("integrity.outside_workspace.deleted", float64(kinds[2]))
	if n == 0 {
		return
	}
	type dir struct {
		Dir   string `json:"dir"`
		Paths int    `json:"paths"`
	}
	dirs := make([]dir, 0, len(byDir))
	for d, k := range byDir {
		dirs = append(dirs, dir{d, k})
	}
	sort.Slice(dirs, func(i, j int) bool {
		if dirs[i].Paths != dirs[j].Paths {
			return dirs[i].Paths > dirs[j].Paths
		}
		return dirs[i].Dir < dirs[j].Dir
	})
	raw, _ := json.Marshal(struct {
		Changed   int   `json:"changed"`
		Added     int   `json:"added"`
		Deleted   int   `json:"deleted"`
		Dirs      []dir `json:"dirs"`
		Truncated bool  `json:"truncated,omitempty"`
	}{kinds[0], kinds[1], kinds[2], dirs, ch.Count > len(ch.Paths)})
	res.Findings = append(res.Findings, store.Finding{RunID: r.ID, Detector: OutsideWorkspace, DetectorVersion: Versions[OutsideWorkspace],
		Severity: "info", Count: n, Evidence: raw})
}
