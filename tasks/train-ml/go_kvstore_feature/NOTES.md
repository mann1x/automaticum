# go_kvstore_feature notes

- Feature: kvd's prototype API (plain-text values, http.Error text errors, 204 on replace, mux 405s in text,
  no listing) must become the README's v1 API: JSON entries + quoted ETag, JSON errors with codes,
  201/200 on create/replace, If-Match (quoted or bare, 400 on junk) / If-None-Match: *, TTL via ttl_ms with
  `now >= expires_at` expiry everywhere (GET, DELETE, list, conditions), versions restart at 1 after
  delete/expiry, `GET /v1/keys?prefix&limit&after` sorted with `next` only when more follow, JSON 405 + Allow.
- Reference: store gains ExpiresAt, PutOptions{TTL, IfMatch, MatchVersion, IfNoneMatch}, List(prefix, after,
  limit) (keys, more); api routes `/v1/keys/{key...}` and `/v1/keys` without method patterns and
  dispatches on r.Method itself so 405s are JSON.
- Checker: a `main` package under __checker__ drives api.NewHandler(store.New(fakeClock)) through
  httptest.NewServer on loopback (works with --network none); the fake clock makes TTL tests exact.
- Near-misses: `ttl-boundary` (expires only when now > expires_at) and `next-on-full-page` (sets `next`
  whenever a page is full, even when nothing follows).
- Needs an exec /tmp, exactly as go_workerpool (same exec probe, same TMPDIR=/tmp + GOTMPDIR=$WORK fix).
