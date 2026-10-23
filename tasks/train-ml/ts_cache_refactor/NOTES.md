# ts_cache_refactor (refactor, long)

- Seed: `src/cache.ts`, one ResponseCache class with Cache-Control parsing, key normalisation and the Map
  store inlined (behaviour correct). README "Planned restructure (issue #41)" asks for cacheControl.ts
  (parseCacheControl), key.ts (normalizeUrl, cacheKey), lru.ts (LruStore<V>), responseCache.ts, index.ts
  re-exports, and cache.ts reduced to `export * from "./responseCache.ts"` (identity-checked).
- Behaviour change: `maxEntries` LRU bound (get hit + put refresh recency, replace never evicts,
  `stats().evictions`, RangeError on invalid bound). The seed's visible test deep-equals stats without
  `evictions`, so the agent must update it too.
- Checker: 8 preserved-behaviour checks (pass on the seed), 4 module/structure checks, 3 LRU checks. Each
  check imports dynamically, so a missing module fails only its checks.
- Reference: fixtures/good (Map-reinsertion LRU). Near-misses: `fifo-not-lru` (LruStore.get does not
  refresh recency), `case-sensitive-directives` (refactor dropped the toLowerCase on directive names).
