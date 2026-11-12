package daemon

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"slices"
	"sync"
	"time"
)

// A server that listens only on loopback cannot be reached from a lane's
// container. An endpoint with forward_to has the daemon listen on its
// base_url's address, the Docker bridge's gateway, and forward each
// connection to the server (030 §4.3). The forwards follow the
// configuration: started when it names one, stopped when it no longer
// does; a connection open when a forward stops is left to finish.

// forward is one listening address and the server it forwards to.
type forward struct {
	listen, target string
	endpoints      []string
	ln             net.Listener
	// err is why it does not listen, until the next try.
	err error
}

// ForwardState is a forward as GET /v1/config shows it.
type ForwardState struct {
	Listen    string   `json:"listen"`
	Target    string   `json:"target"`
	Endpoints []string `json:"endpoints"`
	Error     string   `json:"error,omitempty"`
}

// forwardEvery is how often a forward that could not listen is tried again.
const forwardEvery = 30 * time.Second

// forwardLoop keeps the forwards in line with the configuration until ctx
// ends, then stops them.
func (d *Daemon) forwardLoop(ctx context.Context) {
	defer d.stopForwards()
	for {
		d.syncForwards()
		select {
		case <-ctx.Done():
			return
		case <-time.After(forwardEvery):
		}
	}
}

// syncForwards starts the forwards the configuration names and is not
// running, and stops those it no longer names. It returns what could not
// listen.
func (d *Daemon) syncForwards() []string {
	want := map[string]*forward{}
	for _, n := range d.Local.EndpointNames() {
		e := d.Local.Endpoint(n)
		if e == nil || e.ForwardTo == "" {
			continue
		}
		listen, err := e.ForwardListen()
		if err != nil {
			continue
		}
		if f := want[listen]; f != nil {
			f.endpoints = append(f.endpoints, n)
			continue
		}
		want[listen] = &forward{listen: listen, target: e.ForwardTo, endpoints: []string{n}}
	}
	d.fwdMu.Lock()
	defer d.fwdMu.Unlock()
	if d.forwards == nil {
		d.forwards = map[string]*forward{}
	}
	for listen, f := range d.forwards {
		if w := want[listen]; w == nil || w.target != f.target {
			if f.ln != nil {
				f.ln.Close()
				d.Log.Info("forward stopped", "listen", f.listen, "target", f.target)
			}
			delete(d.forwards, listen)
		}
	}
	var problems []string
	for listen, w := range want {
		f := d.forwards[listen]
		if f == nil {
			f = w
			d.forwards[listen] = f
		}
		f.endpoints = w.endpoints
		if f.ln != nil {
			continue
		}
		ln, err := net.Listen("tcp", listen)
		if err != nil {
			if f.err == nil || f.err.Error() != err.Error() {
				d.Log.Error("forward cannot listen", "listen", listen, "target", f.target, "endpoints", f.endpoints, "error", err)
			}
			f.err = err
			problems = append(problems, fmt.Sprintf("endpoint %s: cannot forward %s to %s: %v", f.endpoints[0], listen, f.target, err))
			continue
		}
		f.ln, f.err = ln, nil
		d.Log.Info("forward started", "listen", listen, "target", f.target, "endpoints", f.endpoints)
		go serveForward(ln, f.target)
	}
	slices.Sort(problems)
	return problems
}

// stopForwards closes every forward's listener.
func (d *Daemon) stopForwards() {
	d.fwdMu.Lock()
	defer d.fwdMu.Unlock()
	for listen, f := range d.forwards {
		if f.ln != nil {
			f.ln.Close()
		}
		delete(d.forwards, listen)
	}
}

// forwardStates lists the forwards, by address.
func (d *Daemon) forwardStates() []ForwardState {
	d.fwdMu.Lock()
	defer d.fwdMu.Unlock()
	var out []ForwardState
	for _, f := range d.forwards {
		s := ForwardState{Listen: f.listen, Target: f.target, Endpoints: slices.Clone(f.endpoints)}
		if f.err != nil {
			s.Error = f.err.Error()
		}
		out = append(out, s)
	}
	slices.SortFunc(out, func(a, b ForwardState) int {
		if a.Listen < b.Listen {
			return -1
		}
		if a.Listen > b.Listen {
			return 1
		}
		return 0
	})
	return out
}

// serveForward accepts until the listener is closed, and copies each
// connection both ways to the target.
func serveForward(ln net.Listener, target string) {
	for {
		c, err := ln.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			time.Sleep(100 * time.Millisecond)
			continue
		}
		go func() {
			defer c.Close()
			up, err := net.DialTimeout("tcp", target, 10*time.Second)
			if err != nil {
				return
			}
			defer up.Close()
			var wg sync.WaitGroup
			wg.Add(2)
			pipe := func(dst, src net.Conn) {
				defer wg.Done()
				io.Copy(dst, src)
				// The other way may still be answering: close only
				// this direction.
				if tc, ok := dst.(*net.TCPConn); ok {
					tc.CloseWrite()
				}
			}
			go pipe(up, c)
			go pipe(c, up)
			wg.Wait()
		}()
	}
}
