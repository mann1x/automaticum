# manic_miner

A browser game in one HTML file that does not run. The agent is asked to make
it work. The verdict comes from an oracle that loads the page in a stub
document, starts the game, pumps 400 animation frames and then tests 28 rules
of the game (landing, running, jumping, walls, items, oxygen, enemies, and
since v6 where a cavern starts the player, moving on to the next cavern, running
out of air, RESTART, and that the player is drawn). Any warning fails.

| file | what it is |
|---|---|
| `seed/manic_miner.html` | the broken page the agent starts from. It does not parse, and has CRLF line endings; both are part of the task |
| `oracle/run_game.logic.js` | the oracle's source. Never placed in a workspace |
| `oracle/build.mjs` | builds the oracle into a single executable whose source cannot be read from the binary; the same source and Node give the same bytes |
| `fixtures/known-good.html` | a working page: a run of the old harness, completed in v6 with a spawn point, moving on to the next cavern, O2 death and a drawn player |
| `fixtures/known-good-renamed-context.html` | known-good with its canvas context renamed: the checks follow behaviour, not names |
| `fixtures/known-bad-weak-jump.html` | known-good with one number changed (the jump is too weak): fails `jump` |
| `fixtures/known-bad-v5-unplayable.html` | v5's known-good: spawns inside a tile, never leaves cavern 1, ignores O2 0, invisible player |
| `fixtures/known-bad-<check>.html` | known-good with one v6 fix taken out, so it fails that check: `no-spawn`, `no-advance`, `no-death`, `death-no-overlay`, `restart-keeps-o2`, `no-draw`, `level-off-by-one`, and `sx-no-retry` (warnings only, so it shows a warning fails) |

The seed is also a fixture: it must fail.

`automaticum task build manic_miner` builds the oracle and checks its hash
against `task.toml`. `automaticum oracle verify manic_miner` proves the oracle
passes the known-good page and fails the others.

The oracle's source is public on purpose (decision D53). Hiding it inside the
binary only keeps an agent from reading the answer in its own workspace.
