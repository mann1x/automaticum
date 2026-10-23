import { BookingError } from "./errors.ts";
import { type Slot, type WeeklyRule, occurrences } from "./recurrence.ts";
import { dayKey, formatInstant, parseDay, parseInstant } from "./time.ts";

export type Booking = {
	id: string;
	room: string;
	who: string;
	start: number;
	end: number;
	seriesId?: string;
};

function overlaps(a: Slot, b: Slot): boolean {
	return a.start <= b.end && b.start <= a.end;
}

export class Calendar {
	private bookings: Booking[] = [];
	private nextId = 1;

	private conflictFor(room: string, slot: Slot): Booking | undefined {
		return this.bookings.find((b) => b.room === room && overlaps(b, slot));
	}

	private check(room: string, slot: Slot): void {
		if (slot.end <= slot.start) {
			throw new BookingError("INVALID_RANGE", "a booking must end after it starts");
		}
		const clash = this.conflictFor(room, slot);
		if (clash) {
			throw new BookingError(
				"CONFLICT",
				`${room} is taken ${formatInstant(clash.start)}..${formatInstant(clash.end)} by ${clash.who}`,
			);
		}
	}

	book(room: string, start: string, end: string, who: string): Booking {
		const slot = { start: parseInstant(start), end: parseInstant(end) };
		this.check(room, slot);
		const booking: Booking = { id: `b${this.nextId++}`, room, who, ...slot };
		this.bookings.push(booking);
		return { ...booking };
	}

	/** Book every occurrence of a weekly rule, or none of them. */
	bookSeries(room: string, rule: WeeklyRule, who: string): Booking[] {
		const slots = occurrences(rule);
		for (const slot of slots) this.check(room, slot);
		const seriesId = `s${this.nextId}`;
		const made = slots.map((slot) => ({ id: `b${this.nextId++}`, room, who, seriesId, ...slot }));
		this.bookings.push(...made);
		return made.map((b) => ({ ...b }));
	}

	cancel(id: string): Booking {
		const index = this.bookings.findIndex((b) => b.id === id);
		const [removed] = this.bookings.splice(index, 1);
		return removed;
	}

	/** Bookings of a room that start on the given day, earliest first. */
	agenda(room: string, day: string): Booking[] {
		parseDay(day);
		return this.bookings
			.filter((b) => b.room === room && dayKey(b.start) === day)
			.sort((a, b) => a.start - b.start)
			.map((b) => ({ ...b }));
	}

	all(): Booking[] {
		return this.bookings.map((b) => ({ ...b }));
	}
}
