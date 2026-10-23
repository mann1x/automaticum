# sparse — a small CSR sparse-matrix library (C++17)

```sh
make            # libsparse.a + the spinfo tool
make test       # builds and runs tests/smoke
./spinfo m.mtx  # shape, stored entries and diagonal of a Matrix Market file
```

Headers: `include/sparse/csr.hpp` (the matrix), `include/sparse/mm_io.hpp` (Matrix Market I/O).
Everything lives in `namespace sparse`. Only the standard library is used.

## CsrMatrix

A `rows x cols` matrix of `double` stored as three arrays: `row_ptr` (size `rows + 1`), `col_idx`
and `values` (both size `nnz`). Row `r`'s entries are `col_idx/values[row_ptr[r] .. row_ptr[r+1])`.

**Canonical form.** Every `CsrMatrix` — whatever function produced it — keeps these invariants:

1. `row_ptr[0] == 0`, `row_ptr` is non-decreasing and `row_ptr[rows] == nnz`;
2. inside a row the column indices are **strictly increasing** (sorted, no duplicates) and `< cols`;
3. **no stored value is 0.0**: an entry whose value is (or sums, or cancels, to) exactly zero is not stored.

Because of this, `operator==` (same shape and identical arrays) is matrix equality.

Already implemented: the constructors (`CsrMatrix()` is 0x0, `CsrMatrix(r, c)` is all zero),
`from_triplets` (sums duplicates, drops zeros, `std::out_of_range` for an index outside the shape),
`identity`, `at` (`std::out_of_range` outside the shape), `to_dense`, `triplets` (row-major order),
`operator==`.

To implement (`src/csr_ops.cpp` currently throws `std::logic_error`):

| member | result |
|---|---|
| `transpose()` | the `cols x rows` transpose |
| `multiply(const std::vector<double>& x)` | `y = A x`, a vector of size `rows`; `std::invalid_argument` if `x.size() != cols` |
| `multiply(const CsrMatrix& b)` | `A B` (`rows x b.cols`); `std::invalid_argument` if `cols != b.rows` |
| `add(const CsrMatrix& b, double alpha = 1.0)` | `A + alpha * B`; `std::invalid_argument` unless the shapes are equal |
| `prune(double tol)` | a copy without the entries whose absolute value is `<= tol`; `std::invalid_argument` if `tol < 0` |
| `diagonal()` | `min(rows, cols)` values `A(i, i)` (0.0 where nothing is stored) |

All of them return canonical matrices (see above) and leave `*this` unchanged. Matrices can have
zero rows or columns.

## Matrix Market I/O

`write_matrix_market(out, m)` (implemented) writes

```
%%MatrixMarket matrix coordinate real general
<rows> <cols> <nnz>
<i> <j> <value>          one line per stored entry, row-major, 1-based, value printed with %.17g
```

`read_matrix_market(in)` (to implement) reads the **coordinate** format:

* Line 1 is the header `%%MatrixMarket matrix coordinate <field> <symmetry>`, five blank-separated
  words compared case-insensitively. `<field>` is `real`, `integer` or `pattern`; `<symmetry>` is
  `general`, `symmetric` or `skew-symmetric`. Anything else (including the `array` format or the
  `complex` field) is an error.
* After the header, lines that start with `%` and blank lines are ignored everywhere. A `\r` at the
  end of a line is ignored.
* The first other line is the size line: exactly three non-negative integers `rows cols nnz`.
  `symmetric` and `skew-symmetric` matrices must be square.
* Then exactly `nnz` entry lines follow: `i j value` (1-based indices, `1 <= i <= rows`,
  `1 <= j <= cols`); for `pattern` there is no value and it is 1.0; for `integer` the value must be an
  integer. Tokens are separated by blanks; a line with the wrong number of tokens or a token that is not
  a number is an error. Entries listed more than once are summed (and dropped if they sum to zero).
* `symmetric`: an off-diagonal entry `(i, j, v)` also stands for `(j, i, v)`; a diagonal entry stands
  only for itself. `skew-symmetric`: an off-diagonal entry `(i, j, v)` also stands for `(j, i, -v)`;
  a diagonal entry is an error.
* Fewer entry lines than `nnz` (end of input) or any further non-comment line after the last entry is
  an error.

Every error is reported by throwing `sparse::ParseError`, whose `line()` is the 1-based line number of
the offending line; for a missing size line or missing entries at the end of input it is the number of
lines in the input plus one. An empty input is an error on line 1.

`load_matrix_market(path)` / `save_matrix_market(path, m)` wrap the stream functions for files.
