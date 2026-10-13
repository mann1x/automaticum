# manic_miner

A browser game in one HTML file that does not run. The agent is asked to make
it work. The verdict comes from an oracle that loads the page in a stub
document, starts the game, pumps 400 animation frames and then tests 23 rules
of the game (landing, running, jumping, walls, items, oxygen, enemies).

| file | what it is |
|---|---|
| `seed/manic_miner.html` | the broken page the agent starts from. It does not parse, and has CRLF line endings; both are part of the task |
| `oracle/run_game.logic.js` | the oracle's source. Never placed in a workspace |
| `oracle/build.mjs` | builds the oracle into a single executable whose source cannot be read from the binary; the same source and Node give the same bytes |
| `fixtures/known-good.html` | a working page: the result of a run of the old harness, kept unchanged |
| `fixtures/known-bad-weak-jump.html` | the working page with one number changed (the jump is too weak): it loads and plays, and fails one rule |

The seed is also a fixture: it must fail.

`automaticum task build manic_miner` builds the oracle and checks its hash
against `task.toml`. `automaticum oracle verify manic_miner` proves the oracle
passes the known-good page and fails the others.

The oracle's source is public on purpose (decision D53). Hiding it inside the
binary only keeps an agent from reading the answer in its own workspace.
