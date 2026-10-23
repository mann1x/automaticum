# rust_cli_buildconfig — notes

- Seed: a Cargo workspace (root Cargo.toml, crates/csvcore library, crates/csvtool CLI, examples/stock.csv,
  visible crates/csvcore/tests/reader.rs). README.md defines the CSV format, library API and CLI.
- Planted build defects (each surfaces only after the previous one is fixed):
  1. workspace `members` lists `crates/cli` (dir is `crates/csvtool`);
  2. csvcore `[lib] path = "src/csv.rs"` (file is src/lib.rs);
  3. csvtool-cli has no `edition` -> 2015 -> `use csvcore::...` unresolved;
  4. the CLI uses `csvcore::stats`, gated by feature `stats`, which its dependency does not enable;
  5. no `[[bin]]`: the binary is `csvtool-cli`, README promises `csvtool`.
  Code defects: `parse_line` ends a quoted field at the first `"` (no `""` escape); `column_sum` errors on
  empty cells instead of skipping them.
- Reference (fixtures/good): fix the member path, drop `[lib]`, `edition = "2021"`, `features = ["stats"]`,
  `[[bin]] name = "csvtool"`, handle `""`, skip empty cells.
- Near-misses: `sum-keeps-empty-cells` (all build fixes + quote fix; fails lib_column_sum, cli_sum);
  `binary-misnamed` (everything but `[[bin]]`; all 7 cli_* checks fail).
- Checker: `cargo build --offline` at the root, then rustc compiles __checker__/check.rs against
  target/debug/libcsvcore.rlib (compile errors print only rustc headlines) and runs it: 5 library checks +
  7 CLI checks that run target/debug/csvtool on files written under $TMPDIR/csvchk.
- **Needs exec /tmp.** With `--tmp-noexec`: seed fails at "cargo build" as before; fixtures that build give
  `"ok":false`, `could not run ./chk: spawnSync ./chk EACCES` (clean verdict, <1 s).
