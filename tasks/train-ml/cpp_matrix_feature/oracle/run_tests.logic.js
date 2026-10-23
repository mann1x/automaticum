const SPEC = {
 "env": {
  "ASAN_OPTIONS": "detect_leaks=0:abort_on_error=0",
  "UBSAN_OPTIONS": "print_stacktrace=0"
 },
 "files": {
  "__checker__/check.cpp": "// Hidden checks for the sparse library. Each check runs in a forked child (built with ASan/UBSan),\n// so a crash or memory error fails only that check.\n#include <cxxabi.h>\n#include <sys/wait.h>\n#include <unistd.h>\n\n#include <cmath>\n#include <csignal>\n#include <cstdarg>\n#include <cstdio>\n#include <cstdlib>\n#include <cstring>\n#include <exception>\n#include <sstream>\n#include <stdexcept>\n#include <string>\n#include <typeinfo>\n#include <vector>\n\n#include \"sparse/csr.hpp\"\n#include \"sparse/mm_io.hpp\"\n\nusing sparse::CsrMatrix;\nusing sparse::Triplet;\nusing Dense = std::vector<std::vector<double>>;\n\nnamespace {\n\nint msg_fd = -1;\n\n[[noreturn]] void fail(const char* fmt, ...)\n{\n\tchar buf[400];\n\tva_list ap;\n\tva_start(ap, fmt);\n\tstd::vsnprintf(buf, sizeof buf, fmt, ap);\n\tva_end(ap);\n\tif (write(msg_fd, buf, std::strlen(buf)) < 0) {\n\t}\n\t_exit(1);\n}\n\nstd::string tname(const std::exception& e)\n{\n\tint status = 0;\n\tchar* d = abi::__cxa_demangle(typeid(e).name(), nullptr, nullptr, &status);\n\tstd::string out = (status == 0 && d) ? d : typeid(e).name();\n\tstd::free(d);\n\treturn out;\n}\n\nstd::string shape(const CsrMatrix& m)\n{\n\treturn std::to_string(m.rows()) + \"x\" + std::to_string(m.cols());\n}\n\n// The README's canonical-form invariants.\nvoid expect_canonical(const char* what, const CsrMatrix& m)\n{\n\tconst auto& rp = m.row_ptr();\n\tconst auto& ci = m.col_idx();\n\tconst auto& v = m.values();\n\tif (rp.size() != m.rows() + 1)\n\t\tfail(\"%s: row_ptr has %zu elements for %zu rows\", what, rp.size(), m.rows());\n\tif (ci.size() != v.size())\n\t\tfail(\"%s: col_idx and values differ in size\", what);\n\tif (rp[0] != 0 || rp[m.rows()] != v.size())\n\t\tfail(\"%s: row_ptr must start at 0 and end at nnz\", what);\n\tfor (std::size_t r = 0; r < m.rows(); ++r) {\n\t\tif (rp[r] > rp[r + 1])\n\t\t\tfail(\"%s: row_ptr decreases at row %zu\", what, r);\n\t\tfor (std::size_t k = rp[r]; k < rp[r + 1]; ++k) {\n\t\t\tif (ci[k] >= m.cols())\n\t\t\t\tfail(\"%s: column index %zu out of range in row %zu\", what, ci[k], r);\n\t\t\tif (k > rp[r] && ci[k] <= ci[k - 1])\n\t\t\t\tfail(\"%s: column indices not strictly increasing in row %zu (%zu after %zu)\", what, r,\n\t\t\t\t     ci[k], ci[k - 1]);\n\t\t\tif (v[k] == 0.0)\n\t\t\t\tfail(\"%s: an explicit zero is stored at (%zu, %zu)\", what, r, ci[k]);\n\t\t}\n\t}\n}\n\nvoid expect_dense(const char* what, const CsrMatrix& m, const Dense& want)\n{\n\texpect_canonical(what, m);\n\tconst std::size_t rows = want.size(), cols = rows ? want[0].size() : m.cols();\n\tif (m.rows() != rows || m.cols() != cols)\n\t\tfail(\"%s: expected a %zux%zu matrix, got %s\", what, rows, cols, shape(m).c_str());\n\tfor (std::size_t r = 0; r < rows; ++r)\n\t\tfor (std::size_t c = 0; c < cols; ++c)\n\t\t\tif (m.at(r, c) != want[r][c])\n\t\t\t\tfail(\"%s: entry (%zu, %zu): expected %g, got %g\", what, r, c, want[r][c], m.at(r, c));\n}\n\n// Deterministic pseudo-random matrix with small integer values (exact arithmetic).\nCsrMatrix random_matrix(std::size_t rows, std::size_t cols, unsigned seed, int density_pct = 35)\n{\n\tstd::vector<Triplet> t;\n\tunsigned s = seed * 2654435761u + 12345u;\n\tfor (std::size_t r = 0; r < rows; ++r)\n\t\tfor (std::size_t c = 0; c < cols; ++c) {\n\t\t\ts = s * 1103515245u + 12345u;\n\t\t\tif (static_cast<int>((s >> 16) % 100) < density_pct) {\n\t\t\t\ts = s * 1103515245u + 12345u;\n\t\t\t\tint v = static_cast<int>((s >> 16) % 7) - 3;\n\t\t\t\tif (v == 0)\n\t\t\t\t\tv = 4;\n\t\t\t\tt.push_back({r, c, static_cast<double>(v)});\n\t\t\t}\n\t\t}\n\treturn CsrMatrix::from_triplets(rows, cols, t);\n}\n\nDense dense_mul(const Dense& a, const Dense& b, std::size_t n, std::size_t k, std::size_t m)\n{\n\tDense c(n, std::vector<double>(m, 0.0));\n\tfor (std::size_t i = 0; i < n; ++i)\n\t\tfor (std::size_t j = 0; j < m; ++j)\n\t\t\tfor (std::size_t q = 0; q < k; ++q)\n\t\t\t\tc[i][j] += a[i][q] * b[q][j];\n\treturn c;\n}\n\ntemplate <class E, class F>\nvoid expect_throw(const char* what, const char* type, F fn)\n{\n\ttry {\n\t\tfn();\n\t} catch (const E&) {\n\t\treturn;\n\t} catch (const std::exception& e) {\n\t\tfail(\"%s: expected %s, got %s (%s)\", what, type, tname(e).c_str(), e.what());\n\t}\n\tfail(\"%s: expected %s, nothing was thrown\", what, type);\n}\n\nCsrMatrix mm_read(const char* what, const std::string& text)\n{\n\tstd::istringstream in(text);\n\ttry {\n\t\treturn sparse::read_matrix_market(in);\n\t} catch (const sparse::ParseError& e) {\n\t\tfail(\"%s: unexpected ParseError: %s\", what, e.what());\n\t}\n}\n\nvoid expect_parse_error(const char* what, const std::string& text, std::size_t line)\n{\n\tstd::istringstream in(text);\n\ttry {\n\t\t(void)sparse::read_matrix_market(in);\n\t} catch (const sparse::ParseError& e) {\n\t\tif (e.line() != line)\n\t\t\tfail(\"%s: expected ParseError on line %zu, got line %zu (%s)\", what, line, e.line(), e.what());\n\t\treturn;\n\t} catch (const std::exception& e) {\n\t\tfail(\"%s: expected sparse::ParseError, got %s (%s)\", what, tname(e).c_str(), e.what());\n\t}\n\tfail(\"%s: expected sparse::ParseError on line %zu, nothing was thrown\", what, line);\n}\n\n// ------------------------------------------------------------------ checks\n\nvoid t_from_triplets()\n{\n\tCsrMatrix m = CsrMatrix::from_triplets(3, 4, {{2, 1, 2.0}, {0, 3, 1.0}, {2, 1, 3.0}, {1, 0, 4.0},\n\t\t\t\t\t\t      {1, 0, -4.0}, {0, 0, 0.0}});\n\texpect_dense(\"from_triplets\", m, {{0, 0, 0, 1}, {0, 0, 0, 0}, {0, 5, 0, 0}});\n\tif (m.nnz() != 2)\n\t\tfail(\"from_triplets: expected nnz 2, got %zu\", m.nnz());\n\texpect_throw<std::out_of_range>(\"from_triplets with row 3 in a 3x4 matrix\", \"std::out_of_range\",\n\t\t\t\t\t[] { CsrMatrix::from_triplets(3, 4, {{3, 0, 1.0}}); });\n\texpect_canonical(\"identity(4)\", CsrMatrix::identity(4));\n}\n\nvoid t_transpose()\n{\n\tCsrMatrix a = random_matrix(7, 5, 1);\n\tDense d = a.to_dense(), want(5, std::vector<double>(7, 0.0));\n\tfor (std::size_t r = 0; r < 7; ++r)\n\t\tfor (std::size_t c = 0; c < 5; ++c)\n\t\t\twant[c][r] = d[r][c];\n\tCsrMatrix t = a.transpose();\n\texpect_dense(\"transpose of a 7x5 matrix\", t, want);\n\tif (t.transpose() != a)\n\t\tfail(\"transpose().transpose() differs from the original\");\n\tif (a != random_matrix(7, 5, 1))\n\t\tfail(\"transpose() modified *this\");\n\tCsrMatrix e = CsrMatrix(0, 3).transpose();\n\tif (e.rows() != 3 || e.cols() != 0 || e.nnz() != 0)\n\t\tfail(\"transpose of a 0x3 matrix: expected 3x0, got %s\", shape(e).c_str());\n\texpect_canonical(\"transpose of a 0x3 matrix\", e);\n\tCsrMatrix z = CsrMatrix::from_triplets(4, 6, {{3, 5, 2.0}, {3, 0, 1.0}}).transpose();\n\texpect_dense(\"transpose with empty rows\", z,\n\t\t     {{0, 0, 0, 1}, {0, 0, 0, 0}, {0, 0, 0, 0}, {0, 0, 0, 0}, {0, 0, 0, 0}, {0, 0, 0, 2}});\n}\n\nvoid t_spmv()\n{\n\tCsrMatrix a = random_matrix(6, 9, 2);\n\tDense d = a.to_dense();\n\tstd::vector<double> x(9);\n\tfor (std::size_t i = 0; i < 9; ++i)\n\t\tx[i] = static_cast<double>(i) - 3.5;\n\tstd::vector<double> y = a.multiply(x);\n\tif (y.size() != 6)\n\t\tfail(\"multiply(vector): expected 6 results, got %zu\", y.size());\n\tfor (std::size_t r = 0; r < 6; ++r) {\n\t\tdouble s = 0;\n\t\tfor (std::size_t c = 0; c < 9; ++c)\n\t\t\ts += d[r][c] * x[c];\n\t\tif (y[r] != s)\n\t\t\tfail(\"multiply(vector): y[%zu] expected %g, got %g\", r, s, y[r]);\n\t}\n\tstd::vector<double> z = CsrMatrix(3, 2).multiply(std::vector<double>{1, 2});\n\tif (z != std::vector<double>{0, 0, 0})\n\t\tfail(\"multiply(vector) on an all-zero 3x2 matrix: expected three zeros\");\n\texpect_throw<std::invalid_argument>(\"multiply(vector) with 8 values for 9 columns\", \"std::invalid_argument\",\n\t\t\t\t\t    [&] { a.multiply(std::vector<double>(8, 1.0)); });\n}\n\nvoid t_spgemm()\n{\n\tCsrMatrix a = random_matrix(5, 7, 3, 45), b = random_matrix(7, 4, 4, 45);\n\texpect_dense(\"5x7 times 7x4\", a.multiply(b), dense_mul(a.to_dense(), b.to_dense(), 5, 7, 4));\n\tCsrMatrix wide = random_matrix(3, 12, 5, 60), tall = random_matrix(12, 10, 6, 60);\n\texpect_dense(\"3x12 times 12x10\", wide.multiply(tall), dense_mul(wide.to_dense(), tall.to_dense(), 3, 12, 10));\n\tif (CsrMatrix::identity(5).multiply(a) != a || a.multiply(CsrMatrix::identity(7)) != a)\n\t\tfail(\"multiplying by the identity changed the matrix\");\n\tCsrMatrix e = CsrMatrix(2, 0).multiply(CsrMatrix(0, 3));\n\tif (e.rows() != 2 || e.cols() != 3 || e.nnz() != 0)\n\t\tfail(\"2x0 times 0x3: expected an empty 2x3 matrix, got %s with %zu entries\", shape(e).c_str(), e.nnz());\n\texpect_canonical(\"2x0 times 0x3\", e);\n\texpect_throw<std::invalid_argument>(\"5x7 times 5x7\", \"std::invalid_argument\", [&] { a.multiply(a); });\n}\n\nvoid t_spgemm_cancellation()\n{\n\tCsrMatrix a = CsrMatrix::from_triplets(2, 2, {{0, 0, 1.0}, {0, 1, 1.0}, {1, 0, 1.0}, {1, 1, 2.0}});\n\tCsrMatrix b = CsrMatrix::from_triplets(2, 3, {{0, 0, 2.0}, {1, 0, -2.0}, {0, 2, 1.0}, {1, 1, 3.0}});\n\tCsrMatrix c = a.multiply(b);\n\texpect_dense(\"product with a cancelling entry\", c, {{0, 3, 1}, {-2, 6, 1}});\n\tif (c.nnz() != 5)\n\t\tfail(\"product with a cancelling entry: expected 5 stored entries, got %zu\", c.nnz());\n\t// columns reached in descending order inside a row must still come out sorted\n\tCsrMatrix p = CsrMatrix::from_triplets(1, 3, {{0, 0, 1.0}, {0, 1, 1.0}, {0, 2, 1.0}});\n\tCsrMatrix q = CsrMatrix::from_triplets(3, 4, {{0, 3, 1.0}, {1, 2, 1.0}, {2, 0, 1.0}, {2, 1, -1.0}, {1, 1, 1.0}});\n\texpect_dense(\"product with unsorted accumulation\", p.multiply(q), {{1, 0, 1, 1}});\n}\n\nvoid t_add()\n{\n\tCsrMatrix a = random_matrix(6, 5, 7), b = random_matrix(6, 5, 8);\n\tDense da = a.to_dense(), db = b.to_dense(), want = da;\n\tfor (std::size_t r = 0; r < 6; ++r)\n\t\tfor (std::size_t c = 0; c < 5; ++c)\n\t\t\twant[r][c] = da[r][c] + 2.5 * db[r][c];\n\texpect_dense(\"A + 2.5 B\", a.add(b, 2.5), want);\n\tfor (std::size_t r = 0; r < 6; ++r)\n\t\tfor (std::size_t c = 0; c < 5; ++c)\n\t\t\twant[r][c] = da[r][c] + db[r][c];\n\texpect_dense(\"A + B\", a.add(b), want);\n\tCsrMatrix z = a.add(a, -1.0);\n\tif (z.rows() != 6 || z.cols() != 5 || z.nnz() != 0)\n\t\tfail(\"A - A: expected an empty 6x5 matrix, got %s with %zu entries\", shape(z).c_str(), z.nnz());\n\tif (a.add(b, 0.0) != a)\n\t\tfail(\"A + 0 * B differs from A\");\n\texpect_throw<std::invalid_argument>(\"adding a 6x5 and a 5x6 matrix\", \"std::invalid_argument\",\n\t\t\t\t\t    [&] { a.add(a.transpose()); });\n}\n\nvoid t_prune()\n{\n\tCsrMatrix a = CsrMatrix::from_triplets(2, 3, {{0, 0, 0.5}, {0, 2, -0.05}, {1, 1, 1e-9}, {1, 2, -2.0}});\n\texpect_dense(\"prune(0.05)\", a.prune(0.05), {{0.5, 0, 0}, {0, 0, -2.0}});\n\tif (a.prune(0.0) != a)\n\t\tfail(\"prune(0) changed the matrix\");\n\tif (a.prune(10.0).nnz() != 0)\n\t\tfail(\"prune(10): expected no entries left\");\n\tif (a.nnz() != 4)\n\t\tfail(\"prune modified *this\");\n\texpect_throw<std::invalid_argument>(\"prune(-1)\", \"std::invalid_argument\", [&] { a.prune(-1.0); });\n}\n\nvoid t_diagonal()\n{\n\tCsrMatrix a = CsrMatrix::from_triplets(3, 5, {{0, 0, 1.0}, {2, 2, 3.0}, {1, 4, 9.0}});\n\tif (a.diagonal() != std::vector<double>{1, 0, 3})\n\t\tfail(\"diagonal of a 3x5 matrix: expected [1, 0, 3]\");\n\tif (a.transpose().diagonal() != std::vector<double>{1, 0, 3})\n\t\tfail(\"diagonal of a 5x3 matrix: expected [1, 0, 3]\");\n\tif (!CsrMatrix(0, 4).diagonal().empty())\n\t\tfail(\"diagonal of a 0x4 matrix: expected no values\");\n}\n\nvoid t_compose()\n{\n\tCsrMatrix a = random_matrix(4, 6, 9, 50), b = random_matrix(6, 5, 10, 50);\n\tif (a.multiply(b).transpose() != b.transpose().multiply(a.transpose()))\n\t\tfail(\"(A B)^T differs from B^T A^T\");\n\tCsrMatrix c = random_matrix(4, 6, 11, 50);\n\tstd::vector<double> x{1, -2, 3, 0.5, -1, 2};\n\tstd::vector<double> lhs = a.add(c).multiply(x), ax = a.multiply(x), cx = c.multiply(x);\n\tfor (std::size_t i = 0; i < 4; ++i)\n\t\tif (lhs[i] != ax[i] + cx[i])\n\t\t\tfail(\"(A + C) x differs from A x + C x at %zu\", i);\n}\n\nvoid t_mm_read_general()\n{\n\tCsrMatrix m = mm_read(\"real general\", \"%%MatrixMarket matrix coordinate real general\\n\"\n\t\t\t\t\t      \"% a comment\\n\"\n\t\t\t\t\t      \"\\n\"\n\t\t\t\t\t      \"3 4 4\\n\"\n\t\t\t\t\t      \"1 1 1.5\\n\"\n\t\t\t\t\t      \"3 4 -2e3\\n\"\n\t\t\t\t\t      \"% comments may appear between entries\\n\"\n\t\t\t\t\t      \"2 2   7\\n\"\n\t\t\t\t\t      \"1 1 0.25\\n\");\n\texpect_dense(\"real general\", m, {{1.75, 0, 0, 0}, {0, 7, 0, 0}, {0, 0, 0, -2000}});\n\tm = mm_read(\"integer, CRLF, mixed-case header\", \"%%matrixmarket MATRIX Coordinate Integer GENERAL\\r\\n\"\n\t\t\t\t\t\t\t\"2 2 3\\r\\n1 2 5\\r\\n2 1 -3\\r\\n2 2 3\\r\\n\");\n\texpect_dense(\"integer, CRLF, mixed-case header\", m, {{0, 5}, {-3, 3}});\n\tm = mm_read(\"pattern\", \"%%MatrixMarket matrix coordinate pattern general\\n2 3 2\\n1 3\\n2 1\\n\");\n\texpect_dense(\"pattern\", m, {{0, 0, 1}, {1, 0, 0}});\n\tm = mm_read(\"entries that cancel\", \"%%MatrixMarket matrix coordinate real general\\n2 2 3\\n1 1 2\\n1 1 -2\\n2 2 1\\n\");\n\texpect_dense(\"entries that cancel\", m, {{0, 0}, {0, 1}});\n\tm = mm_read(\"no entries\", \"%%MatrixMarket matrix coordinate real general\\n0 0 0\\n\");\n\tif (m.rows() != 0 || m.cols() != 0)\n\t\tfail(\"no entries: expected 0x0, got %s\", shape(m).c_str());\n}\n\nvoid t_mm_read_symmetric()\n{\n\tCsrMatrix s = mm_read(\"symmetric\", \"%%MatrixMarket matrix coordinate real symmetric\\n\"\n\t\t\t\t\t   \"3 3 4\\n1 1 2\\n2 1 -1\\n3 2 4\\n3 3 5\\n\");\n\texpect_dense(\"symmetric\", s, {{2, -1, 0}, {-1, 0, 4}, {0, 4, 5}});\n\tCsrMatrix k = mm_read(\"skew-symmetric\", \"%%MatrixMarket matrix coordinate integer skew-symmetric\\n\"\n\t\t\t\t\t\t\"3 3 2\\n2 1 3\\n3 1 -1\\n\");\n\texpect_dense(\"skew-symmetric\", k, {{0, -3, 1}, {3, 0, 0}, {-1, 0, 0}});\n\tCsrMatrix p = mm_read(\"pattern symmetric\", \"%%MatrixMarket matrix coordinate pattern symmetric\\n2 2 2\\n1 1\\n2 1\\n\");\n\texpect_dense(\"pattern symmetric\", p, {{1, 1}, {1, 0}});\n}\n\nvoid t_mm_read_errors()\n{\n\tconst std::string h = \"%%MatrixMarket matrix coordinate real general\\n\";\n\texpect_parse_error(\"empty input\", \"\", 1);\n\texpect_parse_error(\"wrong banner\", \"%MatrixMarket matrix coordinate real general\\n1 1 0\\n\", 1);\n\texpect_parse_error(\"array format\", \"%%MatrixMarket matrix array real general\\n1 1\\n1\\n\", 1);\n\texpect_parse_error(\"complex field\", \"%%MatrixMarket matrix coordinate complex general\\n1 1 0\\n\", 1);\n\texpect_parse_error(\"hermitian\", \"%%MatrixMarket matrix coordinate real hermitian\\n1 1 0\\n\", 1);\n\texpect_parse_error(\"missing size line\", h + \"% only comments\\n\", 3);\n\texpect_parse_error(\"bad size line\", h + \"% c\\n3 x 1\\n1 1 1\\n\", 3);\n\texpect_parse_error(\"size line with 2 numbers\", h + \"3 3\\n\", 2);\n\texpect_parse_error(\"row index 0\", h + \"2 2 2\\n1 1 1\\n0 1 1\\n\", 4);\n\texpect_parse_error(\"column out of range\", h + \"2 2 1\\n\\n1 3 1\\n\", 4);\n\texpect_parse_error(\"missing value\", h + \"2 2 1\\n1 1\\n\", 3);\n\texpect_parse_error(\"extra field\", h + \"2 2 1\\n1 1 1 1\\n\", 3);\n\texpect_parse_error(\"value not a number\", h + \"2 2 1\\n1 1 abc\\n\", 3);\n\texpect_parse_error(\"integer field with a fraction\",\n\t\t\t   \"%%MatrixMarket matrix coordinate integer general\\n2 2 1\\n1 1 1.5\\n\", 3);\n\texpect_parse_error(\"too few entries\", h + \"2 2 3\\n1 1 1\\n2 2 1\\n\", 5);\n\texpect_parse_error(\"too many entries\", h + \"2 2 1\\n1 1 1\\n2 2 1\\n\", 4);\n\texpect_parse_error(\"symmetric but not square\",\n\t\t\t   \"%%MatrixMarket matrix coordinate real symmetric\\n2 3 0\\n\", 2);\n\texpect_parse_error(\"skew-symmetric diagonal\",\n\t\t\t   \"%%MatrixMarket matrix coordinate real skew-symmetric\\n2 2 2\\n2 1 1\\n2 2 1\\n\", 4);\n}\n\nvoid t_mm_roundtrip()\n{\n\tCsrMatrix a = CsrMatrix::from_triplets(3, 4, {{0, 1, 0.1}, {1, 3, 1e-300}, {2, 0, -123456.789},\n\t\t\t\t\t\t      {2, 3, 1.0 / 3.0}, {0, 3, 6.02214076e23}});\n\tstd::ostringstream out;\n\tsparse::write_matrix_market(out, a);\n\tCsrMatrix b = mm_read(\"read back what write_matrix_market wrote\", out.str());\n\tif (b != a)\n\t\tfail(\"write then read does not give the same matrix back\");\n\tCsrMatrix r = random_matrix(9, 8, 12);\n\tstd::ostringstream out2;\n\tsparse::write_matrix_market(out2, r);\n\tif (mm_read(\"round trip of a 9x8 matrix\", out2.str()) != r)\n\t\tfail(\"round trip of a 9x8 matrix differs\");\n}\n\n// ------------------------------------------------------------------ harness\n\nstd::size_t drain(int fd, char* buf, std::size_t cap)\n{\n\tstd::size_t len = 0;\n\tchar tmp[4096];\n\tssize_t n;\n\twhile ((n = read(fd, tmp, sizeof tmp)) > 0) {\n\t\tstd::size_t take = static_cast<std::size_t>(n);\n\t\tif (take > cap - 1 - len)\n\t\t\ttake = cap - 1 - len;\n\t\tstd::memcpy(buf + len, tmp, take);\n\t\tlen += take;\n\t}\n\tbuf[len] = '\\0';\n\treturn len;\n}\n\nstd::string sanitizer_reason(const std::string& err)\n{\n\tstd::size_t e = err.find(\"ERROR: AddressSanitizer: \");\n\tstd::size_t u = err.find(\"runtime error: \");\n\tstd::string kind, where;\n\tif (e != std::string::npos) {\n\t\tstd::size_t s = e + std::strlen(\"ERROR: AddressSanitizer: \");\n\t\tkind = \"memory error (AddressSanitizer): \" + err.substr(s, err.find_first_of(\" \\n\", s) - s);\n\t} else if (u != std::string::npos) {\n\t\tstd::size_t s = u + std::strlen(\"runtime error: \");\n\t\tkind = \"undefined behaviour: \" + err.substr(s, err.find('\\n', s) - s);\n\t} else {\n\t\treturn \"\";\n\t}\n\t// first frame in the library's own sources\n\tstd::size_t pos = 0;\n\twhile ((pos = err.find(\"\\n    #\", pos)) != std::string::npos) {\n\t\tstd::size_t eol = err.find('\\n', pos + 1);\n\t\tstd::string fr = err.substr(pos + 1, eol == std::string::npos ? std::string::npos : eol - pos - 1);\n\t\tpos += 1;\n\t\tif (fr.find(\"__checker__\") != std::string::npos || fr.find(\"libasan\") != std::string::npos ||\n\t\t    fr.find(\"libubsan\") != std::string::npos || fr.find(\".cpp:\") == std::string::npos ||\n\t\t    fr.find(\"/src/\") == std::string::npos)\n\t\t\tcontinue;\n\t\tstd::size_t in = fr.find(\" in \");\n\t\tstd::size_t slash = fr.rfind('/');\n\t\tif (in != std::string::npos && slash != std::string::npos) {\n\t\t\tstd::size_t sp = fr.rfind(' ', slash);\n\t\t\twhere = \" in \" + fr.substr(in + 4, sp - in - 4) + \" (\" + fr.substr(slash + 1) + \")\";\n\t\t\tif (where.size() > 160)\n\t\t\t\twhere = \" at \" + fr.substr(slash + 1);\n\t\t}\n\t\tbreak;\n\t}\n\treturn kind + where;\n}\n\nvoid run(const char* name, void (*fn)())\n{\n\tint mp[2], ep[2], st = 0;\n\tstatic char msg[600], err[65536];\n\tstd::fflush(stdout);\n\tif (pipe(mp) || pipe(ep)) {\n\t\tstd::printf(\"TEST %s FAIL could not create a pipe\\n\", name);\n\t\treturn;\n\t}\n\tpid_t pid = fork();\n\tif (pid == 0) {\n\t\tclose(mp[0]);\n\t\tclose(ep[0]);\n\t\tdup2(ep[1], 2);\n\t\tmsg_fd = mp[1];\n\t\talarm(30);\n\t\ttry {\n\t\t\tfn();\n\t\t} catch (const sparse::ParseError& e) {\n\t\t\tfail(\"unexpected sparse::ParseError: %s\", e.what());\n\t\t} catch (const std::exception& e) {\n\t\t\tfail(\"unexpected exception %s: %s\", tname(e).c_str(), e.what());\n\t\t} catch (...) {\n\t\t\tfail(\"unexpected non-std exception\");\n\t\t}\n\t\t_exit(0);\n\t}\n\tclose(mp[1]);\n\tclose(ep[1]);\n\tdrain(ep[0], err, sizeof err);\n\tdrain(mp[0], msg, sizeof msg);\n\tclose(mp[0]);\n\tclose(ep[0]);\n\twaitpid(pid, &st, 0);\n\tif (WIFEXITED(st) && WEXITSTATUS(st) == 0) {\n\t\tstd::printf(\"TEST %s PASS\\n\", name);\n\t} else if (msg[0]) {\n\t\tstd::printf(\"TEST %s FAIL %s\\n\", name, msg);\n\t} else {\n\t\tstd::string why = sanitizer_reason(err);\n\t\tif (!why.empty())\n\t\t\tstd::printf(\"TEST %s FAIL %s\\n\", name, why.c_str());\n\t\telse if (WIFSIGNALED(st) && WTERMSIG(st) == SIGALRM)\n\t\t\tstd::printf(\"TEST %s FAIL timed out after 30s\\n\", name);\n\t\telse if (WIFSIGNALED(st))\n\t\t\tstd::printf(\"TEST %s FAIL crashed with signal %d\\n\", name, WTERMSIG(st));\n\t\telse\n\t\t\tstd::printf(\"TEST %s FAIL exited with status %d\\n\", name, WEXITSTATUS(st));\n\t}\n\tstd::fflush(stdout);\n}\n\n}  // namespace\n\nint main()\n{\n\trun(\"from_triplets\", t_from_triplets);\n\trun(\"transpose\", t_transpose);\n\trun(\"multiply_vector\", t_spmv);\n\trun(\"multiply_matrix\", t_spgemm);\n\trun(\"multiply_cancellation\", t_spgemm_cancellation);\n\trun(\"add_scaled\", t_add);\n\trun(\"prune\", t_prune);\n\trun(\"diagonal\", t_diagonal);\n\trun(\"operations_compose\", t_compose);\n\trun(\"mm_read_general\", t_mm_read_general);\n\trun(\"mm_read_symmetric\", t_mm_read_symmetric);\n\trun(\"mm_read_errors\", t_mm_read_errors);\n\trun(\"mm_roundtrip\", t_mm_roundtrip);\n\treturn 0;\n}\n"
 },
 "hidden_dir": "__checker__",
 "steps": [
  {
   "cmd": [
    "make"
   ],
   "label": "make",
   "timeout_s": 180
  },
  {
   "cmd": [
    "sh",
    "-c",
    "out=$(g++ -std=c++17 -g -O1 -fno-omit-frame-pointer -fsanitize=address,undefined -fno-sanitize-recover=undefined -Iinclude src/*.cpp __checker__/check.cpp -o chk 2>&1) || { printf '%s\\n' \"$out\" | grep -E '^(src|include)/[^ ]*:[0-9]+:[0-9]+: (fatal )?error'; echo 'the hidden checks do not compile against this library: keep the API of include/sparse/*.hpp (README.md)'; exit 1; }"
   ],
   "label": "compile checks",
   "timeout_s": 180
  },
  {
   "cmd": [
    "./chk"
   ],
   "label": "tests",
   "tests": true,
   "timeout_s": 180
  }
 ],
 "tests": [
  "from_triplets",
  "transpose",
  "multiply_vector",
  "multiply_matrix",
  "multiply_cancellation",
  "add_scaled",
  "prune",
  "diagonal",
  "operations_compose",
  "mm_read_general",
  "mm_read_symmetric",
  "mm_read_errors",
  "mm_roundtrip"
 ]
};
// The checker every training task ships as `run_tests`: the agent may run it, and cannot read it.
//
// make_task.py prepends `const SPEC = {...};` (the task's hidden test files, its build and test
// steps, the test names it must see) and build.mjs (manic_miner's) turns the result into a Node
// single executable whose source is masked.
//
// It copies the working directory to a private temporary directory, writes the hidden tests into
// it, runs the steps there with the language's own tools, and prints ONE JSON line:
//
//   {"ok":true|false,"stage":"build"|"test"|"done","passed":N,"total":N,"failures":[...],"output":"..."}
//
// Test programs report one line per test on stdout:  TEST <name> PASS   or   TEST <name> FAIL <message>
// "ok" needs every step to exit 0 and every name in SPEC.tests to report PASS: a test that never
// reports (the program died, or exited early) is a failure. Output lines that point into the hidden
// test directory are dropped, so a build error never prints the tests' source.
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const cp = require("node:child_process");

const HIDDEN = SPEC.hidden_dir || "__checker__";
const MAX_OUT = 6000;

function copyTree(src, dst) {
	fs.mkdirSync(dst, { recursive: true });
	for (const e of fs.readdirSync(src, { withFileTypes: true })) {
		if (e.name === ".git" || e.name === "run_tests" || e.name === HIDDEN) continue;
		const s = path.join(src, e.name);
		const d = path.join(dst, e.name);
		if (e.isDirectory()) copyTree(s, d);
		else if (e.isFile()) fs.copyFileSync(s, d);
	}
}

// A failure message is the test's own words, but an error inside it (a stack, a module path) can name a
// hidden file: any token that contains the hidden directory's name is replaced.
function hide(text) {
	return String(text).replace(new RegExp(`\\S*${HIDDEN}\\S*`, "g"), "<checker>");
}

// A compiler names the file on one line and quotes its source on the next ones ("  12 | code", a "^~~~"
// marker line): after a line that names a hidden file, those quoted lines are dropped as well.
const QUOTED = /^\s*(\d+\s*)?\|/;
const MARKER = /^\s*\|?\s*[\^~\-+]+[\s\^~\-+]*$/;

function scrub(text) {
	const lines = [];
	let quoting = false;
	for (const l of String(text || "").split("\n")) {
		if (l.includes(HIDDEN)) {
			quoting = true;
			continue;
		}
		if (quoting && (QUOTED.test(l) || MARKER.test(l) || /^\s*$/.test(l))) continue;
		quoting = false;
		if (!/^TEST /.test(l)) lines.push(l);
	}
	const out = lines.join("\n").trim();
	return out.length > MAX_OUT ? `${out.slice(0, MAX_OUT)}\n[... output cut]` : out;
}

function finish(res, work) {
	try {
		fs.rmSync(work, { recursive: true, force: true });
	} catch {}
	process.stdout.write(`${JSON.stringify(res)}\n`);
	process.exit(0);
}

const work = fs.mkdtempSync(path.join(os.tmpdir(), "chk-"));
const res = { ok: false, stage: "build", passed: 0, total: SPEC.tests.length, failures: [], output: "" };
try {
	copyTree(process.cwd(), work);
	for (const [rel, body] of Object.entries(SPEC.files)) {
		const p = path.join(work, rel);
		fs.mkdirSync(path.dirname(p), { recursive: true });
		fs.writeFileSync(p, body);
	}
} catch (e) {
	res.output = `could not prepare the check: ${e.message}`;
	finish(res, work);
}

const env = {
	PATH: process.env.PATH || "/usr/local/bin:/usr/bin:/bin",
	HOME: work,
	TMPDIR: work,
	LANG: "C.UTF-8",
	...(SPEC.env || {}),
};
for (const [k, v] of Object.entries(env)) env[k] = String(v).replaceAll("$WORK", work);

const seen = new Map();
let output = "";
for (const step of SPEC.steps) {
	const cmd = step.cmd.map((a) => String(a).replaceAll("$WORK", work));
	const r = cp.spawnSync(cmd[0], cmd.slice(1), {
		cwd: work,
		env,
		encoding: "utf8",
		timeout: (step.timeout_s || 120) * 1000,
		maxBuffer: 64 * 1024 * 1024,
	});
	const text = `${r.stdout || ""}\n${r.stderr || ""}`;
	if (step.tests) {
		res.stage = "test";
		for (const m of String(r.stdout || "").matchAll(/^TEST (\S+) (PASS|FAIL)(?: (.*))?$/gm)) {
			if (!seen.has(m[1]) || m[2] === "FAIL") seen.set(m[1], m[2] === "PASS" ? null : hide(m[3] || "failed").slice(0, 300));
		}
	}
	output += `${scrub(text)}\n`;
	if (r.error && r.error.code === "ETIMEDOUT") {
		res.output = scrub(`${output}\nstep timed out after ${step.timeout_s || 120}s: ${step.label || step.cmd[0]}`);
		break;
	}
	if (r.error) {
		res.output = scrub(`${output}\ncould not run ${cmd[0]}: ${r.error.message}`);
		break;
	}
	if (r.status !== 0 && !step.tests) {
		res.output = scrub(`${output}\n${step.label || step.cmd[0]} failed (exit ${r.status})`);
		break;
	}
	if (step.tests && r.status !== 0 && seen.size === 0) {
		res.output = scrub(`${output}\nthe tests did not run (exit ${r.status})`);
		break;
	}
}
for (const name of SPEC.tests) {
	if (!seen.has(name)) res.failures.push({ test: name, message: "did not run" });
	else if (seen.get(name) !== null) res.failures.push({ test: name, message: seen.get(name) });
	else res.passed += 1;
}
if (res.stage === "test" && !res.output) {
	res.stage = "done";
	res.output = scrub(output);
}
res.ok = res.stage === "done" && res.passed === res.total;
finish(res, work);
