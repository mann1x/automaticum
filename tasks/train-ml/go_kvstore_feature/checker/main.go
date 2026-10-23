package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"time"

	"github.com/northwind/kvd/api"
	"github.com/northwind/kvd/store"
)

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

var t0 = time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

type env struct {
	srv   *httptest.Server
	clock *fakeClock
}

type resp struct {
	status int
	header http.Header
	raw    string
	body   map[string]any
}

func newEnv() *env {
	c := &fakeClock{now: t0}
	return &env{srv: httptest.NewServer(api.NewHandler(store.New(c.Now))), clock: c}
}

func (e *env) do(method, path, body string, hdr ...string) resp {
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, e.srv.URL+path, rd)
	if err != nil {
		panic(err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		panic(fmt.Sprintf("%s %s: %v", method, path, err))
	}
	defer r.Body.Close()
	raw, _ := io.ReadAll(r.Body)
	out := resp{status: r.StatusCode, header: r.Header, raw: string(raw)}
	json.Unmarshal(raw, &out.body)
	return out
}

func (r resp) short() string {
	s := strings.TrimSpace(r.raw)
	if len(s) > 120 {
		s = s[:120] + "..."
	}
	return fmt.Sprintf("%d %q", r.status, s)
}

type failure string

func check(cond bool, format string, args ...any) {
	if !cond {
		panic(failure(fmt.Sprintf(format, args...)))
	}
}

func expectStatus(r resp, want int, what string) {
	check(r.status == want, "%s: expected status %d, got %s", what, want, r.short())
}

func expectJSON(r resp, what string) {
	ct := r.header.Get("Content-Type")
	check(strings.HasPrefix(ct, "application/json"), "%s: expected Content-Type application/json, got %q", what, ct)
	check(r.body != nil, "%s: body is not a JSON object: %q", what, strings.TrimSpace(r.raw))
}

func expectError(r resp, status int, code, what string) {
	expectStatus(r, status, what)
	expectJSON(r, what)
	check(r.body["error"] == code, "%s: expected error code %q, got %v", what, code, r.body["error"])
	msg, _ := r.body["message"].(string)
	check(msg != "", "%s: error JSON has no message", what)
}

func expectEntry(r resp, status int, key, value string, version int, what string) {
	expectStatus(r, status, what)
	expectJSON(r, what)
	check(r.body["key"] == key && r.body["value"] == value && r.body["version"] == float64(version),
		"%s: expected entry {key:%q value:%q version:%d}, got %s", what, key, value, version, strings.TrimSpace(r.raw))
	etag := r.header.Get("ETag")
	check(etag == fmt.Sprintf("%q", fmt.Sprint(version)), "%s: expected ETag %q, got %q", what, fmt.Sprintf("%q", fmt.Sprint(version)), etag)
}

func keysOf(r resp) []string {
	arr, ok := r.body["keys"].([]any)
	check(ok, "list: expected a JSON array in \"keys\", got %s", strings.TrimSpace(r.raw))
	out := make([]string, len(arr))
	for i, k := range arr {
		out[i], _ = k.(string)
	}
	return out
}

func run(name string, f func(e *env)) {
	e := newEnv()
	defer e.srv.Close()
	done := make(chan string, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				if f, ok := r.(failure); ok {
					done <- string(f)
					return
				}
				done <- fmt.Sprintf("panic: %v", r)
			}
		}()
		f(e)
		done <- ""
	}()
	select {
	case msg := <-done:
		if msg != "" {
			fmt.Printf("TEST %s FAIL %s\n", name, strings.ReplaceAll(msg, "\n", " "))
			return
		}
		fmt.Printf("TEST %s PASS\n", name)
	case <-time.After(10 * time.Second):
		fmt.Printf("TEST %s FAIL timed out after 10s\n", name)
	}
}

func main() {
	http.DefaultClient.Timeout = 5 * time.Second

	run("put_get_json", func(e *env) {
		r := e.do("PUT", "/v1/keys/user/42", `{"value":"Ada"}`)
		expectEntry(r, 201, "user/42", "Ada", 1, "PUT of a new key")
		_, has := r.body["expires_at"]
		check(!has, "PUT without ttl_ms: entry must not have expires_at, got %s", strings.TrimSpace(r.raw))
		r = e.do("GET", "/v1/keys/user/42", "")
		expectEntry(r, 200, "user/42", "Ada", 1, "GET")
	})

	run("replace_bumps_version", func(e *env) {
		e.do("PUT", "/v1/keys/color", `{"value":"red"}`)
		r := e.do("PUT", "/v1/keys/color", `{"value":"green"}`)
		expectEntry(r, 200, "color", "green", 2, "second PUT of a key")
		r = e.do("PUT", "/v1/keys/color", `{"value":""}`)
		expectEntry(r, 200, "color", "", 3, "third PUT (empty string value)")
		expectEntry(e.do("GET", "/v1/keys/color", ""), 200, "color", "", 3, "GET after three PUTs")
	})

	run("not_found_json", func(e *env) {
		expectError(e.do("GET", "/v1/keys/missing", ""), 404, "not_found", "GET of a missing key")
		expectError(e.do("DELETE", "/v1/keys/missing", ""), 404, "not_found", "DELETE of a missing key")
	})

	run("delete_and_recreate", func(e *env) {
		e.do("PUT", "/v1/keys/k", `{"value":"1"}`)
		e.do("PUT", "/v1/keys/k", `{"value":"2"}`)
		r := e.do("DELETE", "/v1/keys/k", "")
		expectStatus(r, 204, "DELETE of an existing key")
		check(strings.TrimSpace(r.raw) == "", "DELETE: expected no body, got %q", r.raw)
		expectError(e.do("GET", "/v1/keys/k", ""), 404, "not_found", "GET after DELETE")
		expectEntry(e.do("PUT", "/v1/keys/k", `{"value":"3"}`), 201, "k", "3", 1, "PUT after DELETE")
	})

	run("bad_request", func(e *env) {
		for _, body := range []string{`{"value":`, `not json`, `{}`, `{"value":42}`, `{"value":"x","ttl_ms":-5}`} {
			expectError(e.do("PUT", "/v1/keys/b", body), 400, "bad_request", fmt.Sprintf("PUT with body %s", body))
		}
		expectError(e.do("GET", "/v1/keys/b", ""), 404, "not_found", "GET after only rejected PUTs")
	})

	run("cas_if_match", func(e *env) {
		e.do("PUT", "/v1/keys/acct", `{"value":"100"}`)
		e.do("PUT", "/v1/keys/acct", `{"value":"90"}`)
		expectEntry(e.do("PUT", "/v1/keys/acct", `{"value":"80"}`, "If-Match", `"2"`), 200, "acct", "80", 3, `PUT with If-Match "2" on version 2`)
		expectError(e.do("PUT", "/v1/keys/acct", `{"value":"70"}`, "If-Match", `"2"`), 412, "version_mismatch", `PUT with If-Match "2" on version 3`)
		expectEntry(e.do("GET", "/v1/keys/acct", ""), 200, "acct", "80", 3, "GET after a refused If-Match PUT")
		expectEntry(e.do("PUT", "/v1/keys/acct", `{"value":"60"}`, "If-Match", "3"), 200, "acct", "60", 4, "PUT with bare If-Match 3")
		expectError(e.do("PUT", "/v1/keys/nobody", `{"value":"x"}`, "If-Match", "1"), 412, "version_mismatch", "PUT with If-Match on a missing key")
		expectError(e.do("PUT", "/v1/keys/acct", `{"value":"x"}`, "If-Match", "abc"), 400, "bad_request", "PUT with If-Match: abc")
	})

	run("create_only", func(e *env) {
		expectEntry(e.do("PUT", "/v1/keys/lock", `{"value":"me"}`, "If-None-Match", "*"), 201, "lock", "me", 1, "PUT with If-None-Match * on a new key")
		expectError(e.do("PUT", "/v1/keys/lock", `{"value":"you"}`, "If-None-Match", "*"), 412, "key_exists", "PUT with If-None-Match * on an existing key")
		expectEntry(e.do("GET", "/v1/keys/lock", ""), 200, "lock", "me", 1, "GET after a refused create")
	})

	run("ttl_expiry", func(e *env) {
		r := e.do("PUT", "/v1/keys/session", `{"value":"abc","ttl_ms":1500}`)
		expectEntry(r, 201, "session", "abc", 1, "PUT with ttl_ms 1500")
		exp, _ := r.body["expires_at"].(string)
		ts, err := time.Parse(time.RFC3339Nano, exp)
		check(err == nil && ts.Equal(t0.Add(1500*time.Millisecond)), "expires_at: expected %s, got %q", t0.Add(1500*time.Millisecond).Format(time.RFC3339Nano), exp)
		e.clock.Advance(1499 * time.Millisecond)
		expectEntry(e.do("GET", "/v1/keys/session", ""), 200, "session", "abc", 1, "GET 1499ms after a 1500ms TTL")
		e.clock.Advance(time.Millisecond)
		expectError(e.do("GET", "/v1/keys/session", ""), 404, "not_found", "GET exactly 1500ms after a 1500ms TTL")
		expectError(e.do("DELETE", "/v1/keys/session", ""), 404, "not_found", "DELETE of an expired key")
	})

	run("ttl_replace_and_recreate", func(e *env) {
		e.do("PUT", "/v1/keys/a", `{"value":"1","ttl_ms":1000}`)
		r := e.do("PUT", "/v1/keys/a", `{"value":"2"}`)
		expectEntry(r, 200, "a", "2", 2, "PUT without ttl over a key with TTL")
		_, has := r.body["expires_at"]
		check(!has, "a PUT without ttl_ms must clear the TTL, got %s", strings.TrimSpace(r.raw))
		e.do("PUT", "/v1/keys/b", `{"value":"old","ttl_ms":1000}`)
		e.do("PUT", "/v1/keys/b", `{"value":"old2","ttl_ms":1000}`)
		e.clock.Advance(5 * time.Second)
		expectEntry(e.do("GET", "/v1/keys/a", ""), 200, "a", "2", 2, "GET of the key whose TTL was cleared")
		expectError(e.do("PUT", "/v1/keys/b", `{"value":"x"}`, "If-Match", "2"), 412, "version_mismatch", "If-Match on an expired key")
		expectEntry(e.do("PUT", "/v1/keys/b", `{"value":"new"}`, "If-None-Match", "*"), 201, "b", "new", 1, "If-None-Match * over an expired key")
	})

	run("list_prefix_sorted", func(e *env) {
		for _, k := range []string{"user/b", "user/a", "admin/x", "user/c", "userx"} {
			e.do("PUT", "/v1/keys/"+k, `{"value":"v"}`)
		}
		e.do("PUT", "/v1/keys/user/0", `{"value":"v","ttl_ms":10}`)
		e.clock.Advance(time.Second)
		r := e.do("GET", "/v1/keys?prefix=user/", "")
		expectStatus(r, 200, "list with prefix user/")
		expectJSON(r, "list")
		got := strings.Join(keysOf(r), ",")
		check(got == "user/a,user/b,user/c", "list prefix=user/: expected keys user/a,user/b,user/c, got %s", got)
		_, has := r.body["next"]
		check(!has, "list: \"next\" must be absent when no keys follow, got %s", strings.TrimSpace(r.raw))
		r = e.do("GET", "/v1/keys", "")
		got = strings.Join(keysOf(r), ",")
		check(got == "admin/x,user/a,user/b,user/c,userx", "list without prefix: expected admin/x,user/a,user/b,user/c,userx, got %s", got)
		r = e.do("GET", "/v1/keys?prefix=zzz", "")
		check(strings.Contains(strings.ReplaceAll(r.raw, " ", ""), `"keys":[]`), "list with no match: expected \"keys\":[], got %s", strings.TrimSpace(r.raw))
	})

	run("list_pagination", func(e *env) {
		for _, k := range []string{"p/1", "p/2", "p/3", "p/4", "p/5"} {
			e.do("PUT", "/v1/keys/"+k, `{"value":"v"}`)
		}
		r := e.do("GET", "/v1/keys?prefix=p/&limit=2", "")
		check(strings.Join(keysOf(r), ",") == "p/1,p/2" && r.body["next"] == "p/2", "page 1 (limit=2): expected keys p/1,p/2 next p/2, got %s", strings.TrimSpace(r.raw))
		r = e.do("GET", "/v1/keys?prefix=p/&limit=2&after=p/2", "")
		check(strings.Join(keysOf(r), ",") == "p/3,p/4" && r.body["next"] == "p/4", "page 2: expected keys p/3,p/4 next p/4, got %s", strings.TrimSpace(r.raw))
		r = e.do("GET", "/v1/keys?prefix=p/&limit=3&after=p/2", "")
		_, has := r.body["next"]
		check(strings.Join(keysOf(r), ",") == "p/3,p/4,p/5" && !has, "limit=3 after=p/2: expected keys p/3,p/4,p/5 and no next, got %s", strings.TrimSpace(r.raw))
		for _, lim := range []string{"0", "1001", "x", "-1"} {
			expectError(e.do("GET", "/v1/keys?limit="+lim, ""), 400, "bad_request", "list with limit="+lim)
		}
	})

	run("method_not_allowed", func(e *env) {
		for _, c := range [][2]string{{"POST", "/v1/keys/x"}, {"PATCH", "/v1/keys/x"}, {"DELETE", "/v1/keys"}, {"POST", "/v1/keys"}} {
			r := e.do(c[0], c[1], `{"value":"v"}`)
			expectError(r, 405, "method_not_allowed", c[0]+" "+c[1])
			check(r.header.Get("Allow") != "", "%s %s: 405 without an Allow header", c[0], c[1])
		}
	})
}
