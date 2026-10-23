// Response cache used by the gateway. Grew out of a quick fix in the proxy handler; everything
// lives here for now.

export type CachedResponse = {
	status: number;
	headers: Record<string, string>;
	body: string;
};

export type CacheRequest = { method: string; url: string };

export type CacheOptions = {
	/** TTL used when the response has no max-age / s-maxage (default 60 s). */
	defaultTtlMs?: number;
	/** Clock, for tests. */
	now?: () => number;
};

export type CacheStats = { hits: number; misses: number; size: number };

type Entry = { response: CachedResponse; storedAt: number; ttlMs: number; url: string };

export class ResponseCache {
	private entries = new Map<string, Entry>();
	private hits = 0;
	private misses = 0;
	private defaultTtlMs: number;
	private now: () => number;

	constructor(opts: CacheOptions = {}) {
		this.defaultTtlMs = opts.defaultTtlMs ?? 60_000;
		this.now = opts.now ?? Date.now;
	}

	get(req: CacheRequest): CachedResponse | undefined {
		const m = req.method.toUpperCase();
		if (m !== "GET" && m !== "HEAD") return undefined;
		const u = new URL(req.url);
		u.hash = "";
		u.searchParams.sort();
		const k = `${m} ${u.toString()}`;
		const e = this.entries.get(k);
		if (!e) {
			this.misses++;
			return undefined;
		}
		if (this.now() - e.storedAt >= e.ttlMs) {
			this.entries.delete(k);
			this.misses++;
			return undefined;
		}
		this.hits++;
		return { status: e.response.status, headers: { ...e.response.headers }, body: e.response.body };
	}

	put(req: CacheRequest, res: CachedResponse): boolean {
		const m = req.method.toUpperCase();
		if (m !== "GET" && m !== "HEAD") return false;
		if (res.status !== 200) return false;
		let cc: string | undefined;
		for (const h of Object.keys(res.headers)) {
			if (h.toLowerCase() === "cache-control") cc = res.headers[h];
		}
		let ttl = this.defaultTtlMs;
		if (cc) {
			let maxAge: number | undefined;
			let sMaxAge: number | undefined;
			for (const part of cc.split(",")) {
				const [rawName, rawValue] = part.split("=");
				const name = rawName.trim().toLowerCase();
				if (name === "no-store" || name === "private") return false;
				if (name === "max-age" || name === "s-maxage") {
					const v = rawValue === undefined ? Number.NaN : Number(rawValue.trim().replace(/^"|"$/g, ""));
					if (Number.isInteger(v) && v >= 0) {
						if (name === "max-age") maxAge = v;
						else sMaxAge = v;
					}
				}
			}
			if (sMaxAge !== undefined) ttl = sMaxAge * 1000;
			else if (maxAge !== undefined) ttl = maxAge * 1000;
		}
		if (ttl <= 0) return false;
		const u = new URL(req.url);
		u.hash = "";
		u.searchParams.sort();
		const url = u.toString();
		this.entries.set(`${m} ${url}`, {
			response: { status: res.status, headers: { ...res.headers }, body: res.body },
			storedAt: this.now(),
			ttlMs: ttl,
			url,
		});
		return true;
	}

	/** Drop every entry whose URL starts with the given prefix; returns how many were dropped. */
	invalidate(urlPrefix: string): number {
		let n = 0;
		for (const [k, e] of this.entries) {
			if (e.url.startsWith(urlPrefix)) {
				this.entries.delete(k);
				n++;
			}
		}
		return n;
	}

	stats(): CacheStats {
		return { hits: this.hits, misses: this.misses, size: this.entries.size };
	}
}
