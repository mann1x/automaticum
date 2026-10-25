# tbench2/circuit-fibsqrt

Terminal-Bench 2.0's task `terminal-bench/circuit-fibsqrt` (Apache-2.0, see
`../LICENSE`), imported from commit `2fd12b88aafdd04a52c298e3940bcb189f9766d6` by
`automaticum suite import tbench2` (066). Expert estimate 16h0m0s, junior
40h0m0s; agent timeout 1h0m0s.

- Image: `alexgshaw/circuit-fibsqrt:20251031`, pinned as `alexgshaw/circuit-fibsqrt@sha256:29783439f529eaed2145592f15af7a2281161528860392a382c825489030ae3a`.
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
