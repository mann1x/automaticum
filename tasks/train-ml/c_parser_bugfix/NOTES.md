# c_parser_bugfix — notes

- Seed: libini, a C11 INI parser (src/ini_{parse,doc,get,util}.c, Makefile, inidump tool, visible `make test`
  that passes on the seed). README.md is the spec.
- Planted defects (4, different kinds):
  1. `ini_unescape` bound check `o >= outsz` leaves no room for the NUL -> one-byte heap overflow, returns 3
     instead of INI_ERR_RANGE (buffer bound / off-by-one).
  2. `parse_quoted` stops at the first `"` even when it is escaped (`\"` edge case).
  3. `entry_append` reallocs the value but never stores the new pointer -> use-after-free / double free
     on continuation lines (often invisible without ASan; glibc grows in place).
  4. `ini_get_int` ignores `errno == ERANGE` -> overflow returns INI_OK (wrong error return).
- Reference fix (fixtures/good): `o + 1 >= outsz`; skip `\x` pairs while scanning for the closing quote;
  `e->value = nv`; `if (errno == ERANGE) return INI_ERR_RANGE`.
- Near-misses: `naive-quote-scan` treats a quote preceded by a backslash as escaped (fails on `"a\\"`);
  `range-check-long-max` uses `r == LONG_MAX` (rejects LONG_MAX itself, misses negative overflow).
- Checker: `make`, then compiles src/*.c + __checker__/check.c with -fsanitize=address,undefined
  (ASAN_OPTIONS=detect_leaks=0: no LeakSanitizer/ptrace dependency) and runs each of the 13 checks in a
  forked child, so a sanitizer abort fails only that check, with the kind + library frame in the message.
- **Needs exec /tmp** (runs a compiled test program from $WORK under /tmp). With `--tmp-noexec` every fixture
  still gets a clean verdict in ~1 s: `"ok":false`, `could not run ./chk: spawnSync ./chk EACCES`.
