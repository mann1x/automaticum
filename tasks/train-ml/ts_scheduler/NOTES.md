# ts_scheduler (bug-fix, short)

Planted defects (4, different kinds):
1. Off-by-one (recurrence.ts): the series loop stops at 00:00Z of `until`, so the occurrence ON the
   inclusive `until` day is dropped. Fix: `t < until + DAY_MS` (or compare dayKey <= until).
2. Time zone (time.ts): `dayKey` uses local getters (getFullYear/getMonth/getDate) instead of UTC ones;
   agenda and `skip` land on the wrong day off-UTC. The checker runs with TZ=America/New_York (set in
   checker.json env) -- the visible tests and a UTC shell won't show it ("works on my machine").
3. Edge case (calendar.ts): `overlaps` uses `<=`, so back-to-back [10,11) / [11,12) bookings conflict.
4. Error path (calendar.ts): `cancel` of an unknown id does `splice(-1, 1)` and removes the LAST booking
   instead of throwing BookingError NOT_FOUND.
Near-misses: `local-daykey` fixes 1/3/4 but not the TZ bug; `cancel-plain-error` throws a plain Error
instead of BookingError("NOT_FOUND"). Node 26.11.1 in the pinned image runs .ts via type stripping (verified).
