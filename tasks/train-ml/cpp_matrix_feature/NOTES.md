# cpp_matrix_feature — notes

- Seed: `sparse`, a C++17 CSR matrix library (include/sparse/{csr,mm_io}.hpp, src/csr.cpp done,
  src/csr_ops.cpp + read_matrix_market stubs throwing std::logic_error, spinfo tool, visible `make test`).
- Feature: implement transpose, SpMV, SpGEMM, add(alpha), prune, diagonal and the Matrix Market coordinate
  reader (real/integer/pattern, general/symmetric/skew-symmetric, ParseError with 1-based line numbers),
  all results in the README's canonical form (sorted columns, no stored 0.0).
- Reference (fixtures/good): counting-sort transpose, dense-accumulator SpGEMM with sorted touched columns
  and zero drop, merge-based add, line-tracking reader that builds via from_triplets.
- Near-misses: `symmetric-diagonal-doubled` mirrors diagonal entries too (fails mm_read_symmetric);
  `product-keeps-zeros` stores cancelled SpGEMM entries (fails multiply_matrix + multiply_cancellation).
- Checker: `make`, then g++ -fsanitize=address,undefined of src/*.cpp + __checker__/check.cpp; 13 checks,
  each in a forked child (crash/sanitizer report fails only that check). Integer-valued pseudo-random
  matrices keep every dense comparison exact.
- **Needs exec /tmp.** With `--tmp-noexec` every fixture still gets a clean verdict in ~7 s:
  `"ok":false`, `could not run ./chk: spawnSync ./chk EACCES`.
