# tbench2/feal-linear-cryptanalysis

Terminal-Bench 2.0's task `terminal-bench/feal-linear-cryptanalysis` (Apache-2.0, see
`../LICENSE`), imported from commit `2fd12b88aafdd04a52c298e3940bcb189f9766d6` by
`automaticum suite import tbench2` (066). Expert estimate 16h0m0s, junior
320h0m0s; agent timeout 30m0s.

- Image: `alexgshaw/feal-linear-cryptanalysis:20251031`, pinned as `alexgshaw/feal-linear-cryptanalysis@sha256:ea78749a8422c6228d26c8753f45906a6a2df089906764b7e3c3e27e8f718ee7`.
- Seed: the image's `/app`, copied out.
- Oracle: the task's `tests/` with pytest 8.4.1 and the tests' packages
  (none), run in the same image with no network; the source's
  `tests/test.sh` fetches uv, Python and these at verify time. Its report
  plugin (`pytest-json-ctrf`) is left out: the exit code is the verdict. Vendored:
  `iniconfig-2.3.1-py3-none-any.whl`, `packaging-26.3-py3-none-any.whl`, `pluggy-1.7.0-py3-none-any.whl`, `pygments-2.21.0-py3-none-any.whl`, `pytest-8.4.1-py3-none-any.whl`.
- Gold fixture: what `solution/solve.sh` wrote, run once as root in the image.

Differences from the source's own harness (066 §4): the agent runs as uid
1000, not root; only `/app` reaches the verifier, not the rest of the
container; the source's CPU and memory limits are labels, the lane's own hold.
