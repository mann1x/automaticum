export function checkMaxEntries(maxEntries: number): number {
	if (maxEntries !== Number.POSITIVE_INFINITY && !(Number.isInteger(maxEntries) && maxEntries > 0)) {
		throw new RangeError(`maxEntries must be a positive integer or Infinity, got ${maxEntries}`);
	}
	return maxEntries;
}

/** A map bounded to `maxEntries` keys that evicts the least recently used one. */
export class LruStore<V> {
	private map = new Map<string, V>();
	readonly maxEntries: number;

	constructor(maxEntries: number = Number.POSITIVE_INFINITY) {
		this.maxEntries = checkMaxEntries(maxEntries);
	}

	get size(): number {
		return this.map.size;
	}

	has(key: string): boolean {
		return this.map.has(key);
	}

	get(key: string): V | undefined {
		return this.map.get(key);
	}

	set(key: string, value: V): string | undefined {
		const existed = this.map.delete(key);
		this.map.set(key, value);
		if (existed || this.map.size <= this.maxEntries) return undefined;
		const oldest = this.map.keys().next().value as string;
		this.map.delete(oldest);
		return oldest;
	}

	delete(key: string): boolean {
		return this.map.delete(key);
	}

	keys(): string[] {
		return [...this.map.keys()];
	}
}
