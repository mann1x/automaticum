package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/mann1x/automaticum/internal/human"
	"github.com/mann1x/automaticum/internal/ingest"
)

// runTimeline prints a run's timeline (070 §1.1): one line per turn, with
// its calls and how many failed, and between them every guard trip,
// notice of the SDK, fork, mode switch and the agent's end, each with its
// time and how long after the run's first event it came. With -requests,
// each of the agent's model requests from the wire capture as well: when
// its first byte came, how long it took in all and what the endpoint said
// of it (D139).
func runTimeline(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("run timeline", flag.ExitOnError)
	_, dial := addClient(fs)
	withRequests := fs.Bool("requests", false, "add each of the agent's model requests from the wire capture: first byte, time in all, what the endpoint said")
	rest := parse(fs, args)
	if len(rest) != 1 {
		return errors.New("usage: automaticum run timeline [-requests] <run>")
	}
	c, err := dial()
	if err != nil {
		return err
	}
	resp, err := c.open(ctx, "GET", "/v1/runs/"+url.PathEscape(rest[0])+"/blobs/events", nil)
	if err != nil {
		return err
	}
	ev := &ingest.Events{KeepTimeline: true}
	err = ev.Read(resp.Body)
	resp.Body.Close()
	if err != nil {
		return fmt.Errorf("run %s: %w", rest[0], err)
	}
	if *withRequests {
		resp, err := c.open(ctx, "GET", "/v1/runs/"+url.PathEscape(rest[0])+"/blobs/wire", nil)
		if err != nil {
			return fmt.Errorf("run %s: the wire capture: %w", rest[0], err)
		}
		reqs, err := ingest.ReadWire(resp.Body)
		resp.Body.Close()
		if err != nil {
			return fmt.Errorf("run %s: the wire capture: %w", rest[0], err)
		}
		ev.Timeline = withRequestMoments(ev.Timeline, reqs)
	}
	if c.json {
		return printJSON(ev.Timeline)
	}
	lines := timelineLines(ev.Timeline, ev.First)
	if len(lines) == 0 {
		fmt.Println("the run's event stream holds no moment")
		return nil
	}
	w := table()
	fmt.Fprintln(w, "TIME\tAFTER\tTURN\tWHAT")
	for _, l := range lines {
		fmt.Fprintf(w, "%s\t%s\t%d\t%s\n", l.time, l.after, l.turn, l.what)
	}
	return w.Flush()
}

type timelineLine struct {
	time, after string
	turn        int
	what        string
}

// timelineLines folds a run's moments into lines: a turn's tool calls are
// counted on its line, every other moment has its own.
func timelineLines(ms []ingest.Moment, first time.Time) []timelineLine {
	var out []timelineLine
	at := func(t time.Time) (string, string) {
		if t.IsZero() {
			return "-", "-"
		}
		return human.Time(t), human.Duration(t.Sub(first))
	}
	turnLine := -1
	calls, failed := 0, map[string]int{}
	flush := func() {
		if turnLine < 0 {
			return
		}
		what := fmt.Sprintf("turn %d", out[turnLine].turn)
		if calls > 0 {
			what += fmt.Sprintf(": %d calls", calls)
			if n := sumCounts(failed); n > 0 {
				what += fmt.Sprintf(", %d failed (%s)", n, countList(failed))
			}
		}
		out[turnLine].what = what
		calls, failed = 0, map[string]int{}
	}
	for _, m := range ms {
		t, after := at(m.T)
		switch m.Kind {
		case "turn":
			flush()
			out = append(out, timelineLine{time: t, after: after, turn: m.Turn})
			turnLine = len(out) - 1
		case "tool":
			calls++
			if m.Failed {
				failed[m.What]++
			}
		default:
			what := m.Kind + " " + m.What
			switch m.Kind {
			case "trip":
				what = "guard trip: " + m.What
				// The SDK's notice of the same trip, just before it, says
				// less: the trip line stands for both.
				if f := strings.Fields(m.What); len(f) >= 2 && len(out) > 0 {
					if last := out[len(out)-1]; last.turn == m.Turn && last.what == "notice status."+f[0]+"."+f[1] {
						out = out[:len(out)-1]
					}
				}
			case "notice":
				what = "notice " + m.What
			case "end":
				what = "the agent ended: " + m.What
			}
			out = append(out, timelineLine{time: t, after: after, turn: m.Turn, what: strings.TrimSpace(what)})
		}
	}
	flush()
	return out
}

// requestMetaChars is how much of what the endpoint said a line shows.
const requestMetaChars = 200

// withRequestMoments adds a moment for each of the agent's requests at the
// time it was sent, in the turn of the moment before it.
func withRequestMoments(ms []ingest.Moment, reqs []ingest.Request) []ingest.Moment {
	out := append([]ingest.Moment(nil), ms...)
	for _, q := range reqs {
		if !q.Agent {
			continue
		}
		what := fmt.Sprintf("%d (%d)", q.Seq, q.Status)
		if q.FirstByteMS != nil {
			what += ": first byte " + human.Millis(*q.FirstByteMS) + ","
		} else {
			what += ":"
		}
		what += " " + human.Millis(q.MS) + " in all"
		if len(q.Meta) > 0 && string(q.Meta) != "null" {
			meta := string(q.Meta)
			if r := []rune(meta); len(r) > requestMetaChars {
				meta = string(r[:requestMetaChars]) + "…"
			}
			what += "; endpoint " + meta
		}
		out = append(out, ingest.Moment{T: q.Start, Kind: "request", What: what, Failed: q.Failed})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].T.Before(out[j].T) })
	turn := 0
	for i := range out {
		if out[i].Kind == "request" {
			out[i].Turn = turn
		} else {
			turn = out[i].Turn
		}
	}
	return out
}

func sumCounts(m map[string]int) int {
	n := 0
	for _, v := range m {
		n += v
	}
	return n
}
