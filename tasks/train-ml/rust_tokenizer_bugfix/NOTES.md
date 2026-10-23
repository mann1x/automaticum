# rust_tokenizer_bugfix — notes

- Seed: `exprcalc`, a dependency-free Rust 2021 crate (lexer / parser / eval / error modules, a CLI,
  visible tests/basic.rs that pass on the seed). README.md is the spec.
- Planted defects (4 kinds):
  1. Precedence: `power` loops over `unary` operands -> `^` is left-associative and unary minus binds
     tighter (`2^3^2` = 64, `-2^2` = 4).
  2. Unicode/byte indexing: identifiers accept only ASCII letters and token columns are byte offsets + 1.
  3. Overflow: `*`, unary `-` and `i64::MIN / -1` are unchecked -> debug-build panics.
  4. Wrong error variant: `x % 0` returns `Overflow` (checked_rem's None) instead of `DivisionByZero`.
- Reference fix (fixtures/good): unary -> power -> primary with `^` taking a `unary` exponent; char-counting
  columns + `is_alphabetic`/`is_alphanumeric`; checked_mul/neg/div; explicit zero check for `%`.
- Near-misses: `byte-columns` fixes identifiers but keeps byte columns (fails token_columns, error_columns);
  `unchecked-min-div` leaves `a / b` unchecked (panics on `i64::MIN / -1`, fails overflow).
- Checker: `cargo build`, copies the hidden test to tests/__checker___calc.rs (a compile failure prints only
  rustc headlines + src/ locations), then one #[test] runs 11 named checks under catch_unwind and prints the
  TEST lines (--nocapture). CARGO_HOME/CARGO_TARGET_DIR under $WORK, --offline, RUSTUP_HOME=/usr/local/rustup.
- **Needs exec /tmp** (cargo runs the test binary from $WORK/target). With `--tmp-noexec` every fixture gets a
  clean verdict in ~1 s: `"ok":false`, cargo `Permission denied (os error 13)`, "the tests did not run (exit 101)".
