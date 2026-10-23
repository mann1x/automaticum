# roombook

Meeting-room bookings for the office, with weekly series. TypeScript, no dependencies; runs directly
on Node (>= 23.6, type stripping), so there is no build step. Use only erasable TypeScript syntax
(no `enum`, `namespace` or constructor parameter properties) and import with the `.ts` extension.

```
npm test            # or: node --test test/
```

## Rules

- **Instants** are ISO-8601 strings with an explicit offset (`2024-03-11T10:00:00Z`,
  `2024-03-11T12:00:00+02:00`); anything else throws `BookingError` with code `INVALID_TIME`.
  Internally every instant is epoch milliseconds.
- **Days** are written `YYYY-MM-DD` and are **UTC** calendar days. The library must give the same
  answers whatever time zone the machine running it is set to.
- A booking occupies the half-open interval **[start, end)**. A booking must end after it starts
  (`INVALID_RANGE`). Two bookings of the same room conflict when their intervals share any instant;
  a booking that ends at 10:00 and one that starts at 10:00 do **not** conflict. Different rooms
  never conflict. A conflicting booking throws `CONFLICT` and is not made.
- **Weekly series** (`bookSeries(room, rule, who)`): the first occurrence starts at `rule.start`,
  then one every 7 days (7 x 24 h). `rule.until` is the last day an occurrence may start on --
  **inclusive**. Occurrences starting on a day listed in `rule.skip` are left out. A series is
  booked all-or-nothing: if any occurrence conflicts, `CONFLICT` is thrown and nothing is booked.
- `cancel(id)` removes the booking with that id and returns it. Cancelling an id that does not
  exist throws `BookingError` with code `NOT_FOUND` and changes nothing.
- `agenda(room, day)` lists the room's bookings that start on that (UTC) day, earliest first.

Every `BookingError` has a `code`: `INVALID_TIME`, `INVALID_RANGE`, `CONFLICT` or `NOT_FOUND`.
