# edge-cache

In-process HTTP response cache used by the API gateway. TypeScript, no dependencies; runs directly on
Node (>= 23.6, type stripping) -- no build step. Use only erasable TypeScript syntax (no `enum`,
`namespace` or constructor parameter properties), `import type` / `type` specifiers for type-only
imports, and the `.ts` extension in import paths.

```
npm test            # or: node --test test/
```

## Behaviour

`new ResponseCache({ defaultTtlMs?, now?, maxEntries? })`

- `put(req, res)` stores a response and returns `true`, or returns `false` and stores nothing.
  Only `GET` and `HEAD` requests (method compared case-insensitively) with status `200` are stored.
- `Cache-Control` (header name and directives are case-insensitive, whitespace around directives is
  ignored): `no-store` or `private` -> not stored. The TTL is `s-maxage` if present, else `max-age`,
  else `defaultTtlMs` (default 60 000 ms); values are whole seconds, an invalid value is ignored.
  A TTL of 0 is not stored.
- `get(req)` returns a copy of the stored response while it is fresh, i.e. while
  `now - storedAt < ttl`; at `now - storedAt == ttl` it is stale, is removed and counts as a miss.
  A `get` that finds nothing is a miss; a `get` for a method other than GET/HEAD returns `undefined`
  and is not counted. Changing the returned object never changes the cache.
- **Keys** are `"<METHOD> <normalized url>"`, e.g. `GET https://api.example.com/v1/items?a=1&b=2`.
  The URL is normalized with WHATWG `URL`: scheme and host lower-case, fragment dropped, query
  parameters sorted by name (stable). The path is case-sensitive. GET and HEAD are cached separately.
- `invalidate(urlPrefix)` removes every entry (any method) whose normalized URL starts with
  `urlPrefix`, and returns how many it removed.
- `stats()` returns `{ hits, misses, size, evictions }`.

## Planned restructure (issue #41)

`src/cache.ts` has become hard to change. Split it into these modules and keep all the behaviour
above. The gateway still imports `./cache.ts`, so it stays as a deprecated one-line re-export
(`export * from "./responseCache.ts";`) with no logic of its own:

| module | exports |
|---|---|
| `src/cacheControl.ts` | `parseCacheControl(value: string \| undefined): CacheControl` where `CacheControl = { noStore: boolean; private: boolean; maxAge?: number; sMaxAge?: number }` (seconds; a directive that is absent or invalid is `undefined` / `false`) |
| `src/key.ts` | `normalizeUrl(url: string): string` and `cacheKey(req: CacheRequest): string` (the key format above) |
| `src/lru.ts` | `class LruStore<V>` (below) |
| `src/responseCache.ts` | `class ResponseCache` built on the three modules above |
| `src/index.ts` | re-exports all of the above (and the types) |

### New: bounded size (LRU)

The gateway leaks memory on busy days, so the cache gets a size bound in the same change:

- `maxEntries` (default `Infinity`) must be a positive integer or `Infinity`; anything else makes the
  constructor throw a `RangeError`.
- When a `put` would make the cache hold more than `maxEntries` entries, the **least recently used**
  entry is evicted. Both a `get` hit and a `put` (new or replacing an existing key) make an entry the
  most recently used. Replacing an existing key never evicts. Each eviction adds 1 to
  `stats().evictions`; entries removed because they expired or were invalidated are not evictions.

`LruStore<V>`:

- `new LruStore<V>(maxEntries = Infinity)` -- same validation as above.
- `get(key)` returns the value (or `undefined`) and marks the key most recently used.
- `has(key)` returns whether the key is present, without changing its recency.
- `set(key, value)` stores the value as most recently used and returns the key it evicted, or
  `undefined` if nothing was evicted.
- `delete(key)` returns whether a key was removed.
- `keys()` returns all keys, least recently used first.
- `size` (getter) is the number of keys.
