export type ErrorCode = "INVALID_TIME" | "INVALID_RANGE" | "CONFLICT" | "NOT_FOUND";

export class BookingError extends Error {
	code: ErrorCode;

	constructor(code: ErrorCode, message: string) {
		super(message);
		this.name = "BookingError";
		this.code = code;
	}
}
