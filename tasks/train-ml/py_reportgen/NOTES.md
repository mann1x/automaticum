# py_reportgen (feature, long)

- Feature: README "Report (v0.3)" -- comma-separated multi-key `--group-by` (region/product/channel/month),
  `month` = `YYYY-MM` bucket, `share` column (% of total revenue, half-up to 1 dp), revenue-desc sort with
  key-ascending tie break, TOTAL row, `--format markdown`, unknown key -> argparse error (exit 2),
  empty result -> `no matching records`. Seed only does single-key text sorted by key name.
- Latent bug in seed (README says inclusive): `filters.in_range` treats `--until` as exclusive (leap-day test).
- Reference (fixtures/good): Group carries a key tuple + Decimal share; render.table() builds header/rows/TOTAL
  cells shared by text and markdown renderers; cli parses keys before loading and uses parser.error.
- Near-misses: `float-share` formats share via float `:.1f` (6.25 -> 6.2, half-even); `until-exclusive`
  implements everything but leaves the seed's exclusive `--until`.
- 12 checks, exact output comparison; failure messages give the first differing line.
