# tbench2/regex-chess

Terminal-Bench 2.0's task `terminal-bench/regex-chess` (Apache-2.0, see
`../LICENSE`), imported from commit `2fd12b88aafdd04a52c298e3940bcb189f9766d6` by
`automaticum suite import tbench2` (066). Expert estimate 24h0m0s, junior
80h0m0s; agent timeout 1h0m0s.

- Image: `alexgshaw/regex-chess:20251031`, pinned as `alexgshaw/regex-chess@sha256:e3b6d61d2d3930fbbdc5f7bf091a7f8b7f6125e1951ef8a7dbe165b1ef62942c`.
- Seed: the image's `/app`, copied out.
- Oracle: the task's `tests/` with pytest 8.4.1 and the tests' packages
  (`chess==1.11.2`), run in the same image with no network; the source's
  `tests/test.sh` fetches uv, Python and these at verify time. Its report
  plugin (`pytest-json-ctrf`) is left out: the exit code is the verdict. Vendored:
  `chess-1.11.2-py3-none-any.whl` (built from `chess-1.11.2.tar.gz`, which `oracle/built.txt` pins), `iniconfig-2.3.1-py3-none-any.whl`, `packaging-26.3-py3-none-any.whl`, `pluggy-1.7.0-py3-none-any.whl`, `pygments-2.21.0-py3-none-any.whl`, `pytest-8.4.1-py3-none-any.whl`.
- Gold fixture: what `solution/solve.sh` wrote, run once in the image as the
  agent runs.
- Limits: the source's, 1 CPU and 2048 MB, for the agent's container
  and the oracle's (never more than the lane has).
- The agent runs as the lane's user, uid 1000; the tests as the lane's user.

Differences from the source's own harness (066 §4): only `/app` and what is
declared reach the verifier, not the rest of the container; the verifier is
offline, with its packages pinned.
