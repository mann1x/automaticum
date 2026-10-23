# sh_config_migrate — notes (not shown to the agent)

- Family bug-fix, tier short, bash + mawk. Spec = README.md; seed/examples/old -> examples/new/payments.conf + the
  README report give a runnable example.
- Planted defects: (1) ini2flat.awk strips `[ \t]*[;#].*` from every value before quote handling (kills `#`
  inside quoted values -> "unterminated quote", and cuts `a#b`); (2) a duplicate key keeps the FIRST value and
  is not reported; (3) bool check is case-sensitive (`Yes`, `OFF` rejected); (4) migrate.sh writes the .conf even
  for a file with errors and never removes a stale one.
- Reference (fixtures/good): quote first, then inline comment only on ` ;`/` #` in unquoted values; last value
  wins + warning; `tolower` before bool matching; write-or-`rm -f` per file.
- Near-miss `comment-before-quote`: everything fixed but the inline-comment strip runs before the quote check,
  so `"a ; b"` is cut -> fails `quoted_values` only.
- Checker writes its own mapping + INI files into `__checker__/sandbox/run/` and runs `bash migrate.sh`.
