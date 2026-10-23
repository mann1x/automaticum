export type CacheControl = {
	noStore: boolean;
	private: boolean;
	/** Seconds. */
	maxAge?: number;
	/** Seconds. */
	sMaxAge?: number;
};

function seconds(raw: string | undefined): number | undefined {
	if (raw === undefined) return undefined;
	const v = Number(raw.trim().replace(/^"|"$/g, ""));
	return Number.isInteger(v) && v >= 0 ? v : undefined;
}

export function parseCacheControl(value: string | undefined): CacheControl {
	const cc: CacheControl = { noStore: false, private: false };
	if (!value) return cc;
	for (const part of value.split(",")) {
		const [rawName, rawValue] = part.split("=");
		const name = rawName.trim();
		if (name === "no-store") cc.noStore = true;
		else if (name === "private") cc.private = true;
		else if (name === "max-age") cc.maxAge = seconds(rawValue);
		else if (name === "s-maxage") cc.sMaxAge = seconds(rawValue);
	}
	return cc;
}

/** Header lookup is case-insensitive. */
export function headerValue(headers: Record<string, string>, name: string): string | undefined {
	const want = name.toLowerCase();
	let found: string | undefined;
	for (const h of Object.keys(headers)) {
		if (h.toLowerCase() === want) found = headers[h];
	}
	return found;
}
