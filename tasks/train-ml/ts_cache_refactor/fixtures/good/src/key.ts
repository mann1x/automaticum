export type CacheRequest = { method: string; url: string };

export function normalizeUrl(url: string): string {
	const u = new URL(url);
	u.hash = "";
	u.searchParams.sort();
	return u.toString();
}

export function cacheKey(req: CacheRequest): string {
	return `${req.method.toUpperCase()} ${normalizeUrl(req.url)}`;
}

export function isCacheableMethod(method: string): boolean {
	const m = method.toUpperCase();
	return m === "GET" || m === "HEAD";
}
