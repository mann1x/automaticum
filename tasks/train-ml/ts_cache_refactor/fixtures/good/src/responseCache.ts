import { headerValue, parseCacheControl } from "./cacheControl.ts";
import { type CacheRequest, cacheKey, isCacheableMethod, normalizeUrl } from "./key.ts";
import { LruStore } from "./lru.ts";

export type CachedResponse = {
	status: number;
	headers: Record<string, string>;
	body: string;
};

export type CacheOptions = {
	/** TTL used when the response has no max-age / s-maxage (default 60 s). */
	defaultTtlMs?: number;
	/** Clock, for tests. */
	now?: () => number;
	/** Upper bound on stored entries; least recently used is evicted (default Infinity). */
	maxEntries?: number;
};

export type CacheStats = { hits: number; misses: number; size: number; evictions: number };

type Entry = { response: CachedResponse; storedAt: number; ttlMs: number; url: string };

function copy(res: CachedResponse): CachedResponse {
	return { status: res.status, headers: { ...res.headers }, body: res.body };
}

export class ResponseCache {
	private store: LruStore<Entry>;
	private hits = 0;
	private misses = 0;
	private evictions = 0;
	private defaultTtlMs: number;
	private now: () => number;

	constructor(opts: CacheOptions = {}) {
		this.defaultTtlMs = opts.defaultTtlMs ?? 60_000;
		this.now = opts.now ?? Date.now;
		this.store = new LruStore<Entry>(opts.maxEntries ?? Number.POSITIVE_INFINITY);
	}

	private ttlFor(res: CachedResponse): number | undefined {
		const cc = parseCacheControl(headerValue(res.headers, "cache-control"));
		if (cc.noStore || cc.private) return undefined;
		const seconds = cc.sMaxAge ?? cc.maxAge;
		return seconds === undefined ? this.defaultTtlMs : seconds * 1000;
	}

	get(req: CacheRequest): CachedResponse | undefined {
		if (!isCacheableMethod(req.method)) return undefined;
		const key = cacheKey(req);
		const entry = this.store.get(key);
		if (!entry) {
			this.misses++;
			return undefined;
		}
		if (this.now() - entry.storedAt >= entry.ttlMs) {
			this.store.delete(key);
			this.misses++;
			return undefined;
		}
		this.hits++;
		return copy(entry.response);
	}

	put(req: CacheRequest, res: CachedResponse): boolean {
		if (!isCacheableMethod(req.method) || res.status !== 200) return false;
		const ttlMs = this.ttlFor(res);
		if (ttlMs === undefined || ttlMs <= 0) return false;
		const url = normalizeUrl(req.url);
		const evicted = this.store.set(cacheKey(req), { response: copy(res), storedAt: this.now(), ttlMs, url });
		if (evicted !== undefined) this.evictions++;
		return true;
	}

	/** Drop every entry whose URL starts with the given prefix; returns how many were dropped. */
	invalidate(urlPrefix: string): number {
		let n = 0;
		for (const key of this.store.keys()) {
			const url = key.slice(key.indexOf(" ") + 1);
			if (url.startsWith(urlPrefix)) {
				this.store.delete(key);
				n++;
			}
		}
		return n;
	}

	stats(): CacheStats {
		return { hits: this.hits, misses: this.misses, size: this.store.size, evictions: this.evictions };
	}
}
