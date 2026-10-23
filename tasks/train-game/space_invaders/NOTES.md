# space_invaders — notes

Training twin of manic_miner (same shape: one broken HTML game + a compiled `run_game` oracle; different game, code written fresh).
Planted in `seed/space_invaders.html` (derived from `fixtures/known-good.html`): CRLF line endings; two syntax breaks
(`fireShot` object literal missing a comma, `updateBombs` `if (bomb.y > HEIGHT {`); `buildShields`, `dropBomb`, `nextWave`
deleted but still called (load-time, frame ~245, and wave-clear only); `rectsOverlap` uses `||` between the axes;
`marchFleet` reverses at the edge without stepping down; `marchInterval` counts all aliens, not live ones (no speed-up).
Reference: rebuild shields as 4 bunkers between fleet and cannon, drop bombs from the lowest live alien of a random column
(max 3 in the air), next wave = level+1, fresh fleet 16px lower, countdown again.
Checker (`checker/run_game.logic.js`): stub DOM/canvas, all <script> blocks in one vm context, 400 frames on a fake 16.7ms
clock (countdown ends ~frame 180, witness `player.frameCount`), then 20 behaviour rules each in a fresh seeded context.
Near-misses: `known-bad-no-stepdown` (fails edge_step_down + invasion), `known-bad-no-speedup` (fails speed_up).
No-op stubs of the game functions fail (2-8 of 20 rules pass). No exec /tmp needed.
