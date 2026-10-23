# kvd

A small in-memory key-value store with an HTTP API (Go 1.23, standard library only).

```
store/     the store (thread-safe map + versions); store.New(clock) takes the clock tests use
api/       HTTP handler: api.NewHandler(*store.Store) http.Handler
cmd/kvd/   the server binary (go run ./cmd/kvd -addr 127.0.0.1:8080)
```

Today the API is a prototype (plain-text values, text errors). The clients team needs the **v1 API**
below. `api.NewHandler` and `store.New(store.Clock)` keep their signatures; the rest of the store's
API is yours to change.

## v1 API

Every response that has a body is JSON with `Content-Type: application/json`. Errors always look like

```json
{"error": "<code>", "message": "<human readable text>"}
```

| status | code |
|---|---|
| 400 | `bad_request` |
| 404 | `not_found` |
| 405 | `method_not_allowed` (also set the `Allow` header) |
| 412 | `version_mismatch` / `key_exists` |

Keys are the rest of the path after `/v1/keys/` and may contain slashes (`/v1/keys/team/42/name`).

### Entry JSON

```json
{"key": "user/42", "value": "Ada", "version": 3, "expires_at": "2026-03-01T12:00:01.5Z"}
```

`expires_at` (RFC 3339, UTC) is present only when the key has a TTL. Responses carrying an entry also
set `ETag: "<version>"` (quoted).

### `PUT /v1/keys/{key}`

Body: `{"value": "<string>", "ttl_ms": <integer, optional>}`.

* Malformed JSON, a missing/non-string `value`, or a negative `ttl_ms` -> 400 `bad_request`.
* `ttl_ms` > 0: the key expires `ttl_ms` milliseconds after this write. Absent or 0: no expiry (a write
  without TTL clears an earlier one).
* Versions: a new key starts at version 1; every successful write to an existing key adds 1. A key that
  was deleted or has expired starts again at 1.
* `If-Match: <version>` (quoted or bare): write only if the key exists with exactly that version,
  otherwise 412 `version_mismatch` and nothing changes. A non-numeric version -> 400 `bad_request`.
* `If-None-Match: *`: write only if the key does not exist, otherwise 412 `key_exists`.
* Reply: **201** with the entry when the key was created, **200** with the entry when it was replaced.

### `GET /v1/keys/{key}`

200 with the entry, or 404 `not_found`.

### `DELETE /v1/keys/{key}`

204 with no body, or 404 `not_found`.

### `GET /v1/keys?prefix=<p>&limit=<n>&after=<key>`

Lists live keys starting with `prefix` (default: all), sorted ascending (byte order), only keys strictly
greater than `after` (default: from the start), at most `limit` of them (default 100; a value that is
not an integer in 1..1000 -> 400 `bad_request`).

```json
{"keys": ["user/1", "user/2"], "next": "user/2"}
```

`next` is the last key returned, and is present only when more matching keys follow (pass it as `after`
to get the next page). `keys` is `[]`, never `null`, when nothing matches.

### Expiry

A key with a TTL is gone from the moment `now >= expires_at` (now = the store's clock): GET, DELETE,
listing, `If-Match` and `If-None-Match` all treat it as absent.

### Anything else

Other methods on `/v1/keys/{key}` or `/v1/keys` -> 405 `method_not_allowed` with an `Allow` header.

`go test ./...` runs the few existing tests.
