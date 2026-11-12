package daemon

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mann1x/automaticum/internal/config"
)

func freePort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().String()
}

// An endpoint with forward_to has the daemon listen on base_url's address
// and forward to the server; the forward stops when the configuration no
// longer names it, and an address it cannot take is reported, not fatal.
func TestForward(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "served "+r.URL.Path) }))
	defer srv.Close()
	target := strings.TrimPrefix(srv.URL, "http://")
	listen := freePort(t)
	g := newRig(t, false)
	g.d.Local.Endpoints["srv"] = &config.Endpoint{Name: "srv", Kind: "llamacpp", BaseURL: "http://" + listen + "/v1", ForwardTo: target}
	if bad := g.d.syncForwards(); len(bad) != 0 {
		t.Fatalf("not forwarding: %v", bad)
	}
	resp, err := http.Get("http://" + listen + "/v1/models")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(b) != "served /v1/models" {
		t.Errorf("through the forward: %q", b)
	}
	if st := g.d.configState().Forwards; len(st) != 1 || st[0].Listen != listen || st[0].Error != "" || st[0].Endpoints[0] != "srv" {
		t.Errorf("config view: %+v", st)
	}
	// Taken out of the configuration: the address is let go.
	g.d.Local.Endpoints["srv"] = &config.Endpoint{Name: "srv", Kind: "llamacpp", BaseURL: "http://" + target + "/v1"}
	g.d.syncForwards()
	if c, err := net.Dial("tcp", listen); err == nil {
		c.Close()
		t.Error("the forward still listens after the configuration dropped it")
	}
	// An address someone else holds is reported and tried again later.
	held, err := net.Listen("tcp", listen)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	g.d.Local.Endpoints["srv"] = &config.Endpoint{Name: "srv", Kind: "llamacpp", BaseURL: "http://" + listen + "/v1", ForwardTo: target}
	if bad := g.d.syncForwards(); len(bad) != 1 || !strings.Contains(bad[0], "endpoint srv: cannot forward "+listen) {
		t.Errorf("a taken address: %v", bad)
	}
	if st := g.d.configState().Forwards; len(st) != 1 || st[0].Error == "" {
		t.Errorf("config view of a forward that does not listen: %+v", st)
	}
	held.Close()
	if bad := g.d.syncForwards(); len(bad) != 0 {
		t.Errorf("tried again once the address is free: %v", bad)
	}
	g.d.stopForwards()
}
