// Hidden checks for the sparse library. Each check runs in a forked child (built with ASan/UBSan),
// so a crash or memory error fails only that check.
#include <cxxabi.h>
#include <sys/wait.h>
#include <unistd.h>

#include <cmath>
#include <csignal>
#include <cstdarg>
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <exception>
#include <sstream>
#include <stdexcept>
#include <string>
#include <typeinfo>
#include <vector>

#include "sparse/csr.hpp"
#include "sparse/mm_io.hpp"

using sparse::CsrMatrix;
using sparse::Triplet;
using Dense = std::vector<std::vector<double>>;

namespace {

int msg_fd = -1;

[[noreturn]] void fail(const char* fmt, ...)
{
	char buf[400];
	va_list ap;
	va_start(ap, fmt);
	std::vsnprintf(buf, sizeof buf, fmt, ap);
	va_end(ap);
	if (write(msg_fd, buf, std::strlen(buf)) < 0) {
	}
	_exit(1);
}

std::string tname(const std::exception& e)
{
	int status = 0;
	char* d = abi::__cxa_demangle(typeid(e).name(), nullptr, nullptr, &status);
	std::string out = (status == 0 && d) ? d : typeid(e).name();
	std::free(d);
	return out;
}

std::string shape(const CsrMatrix& m)
{
	return std::to_string(m.rows()) + "x" + std::to_string(m.cols());
}

// The README's canonical-form invariants.
void expect_canonical(const char* what, const CsrMatrix& m)
{
	const auto& rp = m.row_ptr();
	const auto& ci = m.col_idx();
	const auto& v = m.values();
	if (rp.size() != m.rows() + 1)
		fail("%s: row_ptr has %zu elements for %zu rows", what, rp.size(), m.rows());
	if (ci.size() != v.size())
		fail("%s: col_idx and values differ in size", what);
	if (rp[0] != 0 || rp[m.rows()] != v.size())
		fail("%s: row_ptr must start at 0 and end at nnz", what);
	for (std::size_t r = 0; r < m.rows(); ++r) {
		if (rp[r] > rp[r + 1])
			fail("%s: row_ptr decreases at row %zu", what, r);
		for (std::size_t k = rp[r]; k < rp[r + 1]; ++k) {
			if (ci[k] >= m.cols())
				fail("%s: column index %zu out of range in row %zu", what, ci[k], r);
			if (k > rp[r] && ci[k] <= ci[k - 1])
				fail("%s: column indices not strictly increasing in row %zu (%zu after %zu)", what, r,
				     ci[k], ci[k - 1]);
			if (v[k] == 0.0)
				fail("%s: an explicit zero is stored at (%zu, %zu)", what, r, ci[k]);
		}
	}
}

void expect_dense(const char* what, const CsrMatrix& m, const Dense& want)
{
	expect_canonical(what, m);
	const std::size_t rows = want.size(), cols = rows ? want[0].size() : m.cols();
	if (m.rows() != rows || m.cols() != cols)
		fail("%s: expected a %zux%zu matrix, got %s", what, rows, cols, shape(m).c_str());
	for (std::size_t r = 0; r < rows; ++r)
		for (std::size_t c = 0; c < cols; ++c)
			if (m.at(r, c) != want[r][c])
				fail("%s: entry (%zu, %zu): expected %g, got %g", what, r, c, want[r][c], m.at(r, c));
}

// Deterministic pseudo-random matrix with small integer values (exact arithmetic).
CsrMatrix random_matrix(std::size_t rows, std::size_t cols, unsigned seed, int density_pct = 35)
{
	std::vector<Triplet> t;
	unsigned s = seed * 2654435761u + 12345u;
	for (std::size_t r = 0; r < rows; ++r)
		for (std::size_t c = 0; c < cols; ++c) {
			s = s * 1103515245u + 12345u;
			if (static_cast<int>((s >> 16) % 100) < density_pct) {
				s = s * 1103515245u + 12345u;
				int v = static_cast<int>((s >> 16) % 7) - 3;
				if (v == 0)
					v = 4;
				t.push_back({r, c, static_cast<double>(v)});
			}
		}
	return CsrMatrix::from_triplets(rows, cols, t);
}

Dense dense_mul(const Dense& a, const Dense& b, std::size_t n, std::size_t k, std::size_t m)
{
	Dense c(n, std::vector<double>(m, 0.0));
	for (std::size_t i = 0; i < n; ++i)
		for (std::size_t j = 0; j < m; ++j)
			for (std::size_t q = 0; q < k; ++q)
				c[i][j] += a[i][q] * b[q][j];
	return c;
}

template <class E, class F>
void expect_throw(const char* what, const char* type, F fn)
{
	try {
		fn();
	} catch (const E&) {
		return;
	} catch (const std::exception& e) {
		fail("%s: expected %s, got %s (%s)", what, type, tname(e).c_str(), e.what());
	}
	fail("%s: expected %s, nothing was thrown", what, type);
}

CsrMatrix mm_read(const char* what, const std::string& text)
{
	std::istringstream in(text);
	try {
		return sparse::read_matrix_market(in);
	} catch (const sparse::ParseError& e) {
		fail("%s: unexpected ParseError: %s", what, e.what());
	}
}

void expect_parse_error(const char* what, const std::string& text, std::size_t line)
{
	std::istringstream in(text);
	try {
		(void)sparse::read_matrix_market(in);
	} catch (const sparse::ParseError& e) {
		if (e.line() != line)
			fail("%s: expected ParseError on line %zu, got line %zu (%s)", what, line, e.line(), e.what());
		return;
	} catch (const std::exception& e) {
		fail("%s: expected sparse::ParseError, got %s (%s)", what, tname(e).c_str(), e.what());
	}
	fail("%s: expected sparse::ParseError on line %zu, nothing was thrown", what, line);
}

// ------------------------------------------------------------------ checks

void t_from_triplets()
{
	CsrMatrix m = CsrMatrix::from_triplets(3, 4, {{2, 1, 2.0}, {0, 3, 1.0}, {2, 1, 3.0}, {1, 0, 4.0},
						      {1, 0, -4.0}, {0, 0, 0.0}});
	expect_dense("from_triplets", m, {{0, 0, 0, 1}, {0, 0, 0, 0}, {0, 5, 0, 0}});
	if (m.nnz() != 2)
		fail("from_triplets: expected nnz 2, got %zu", m.nnz());
	expect_throw<std::out_of_range>("from_triplets with row 3 in a 3x4 matrix", "std::out_of_range",
					[] { CsrMatrix::from_triplets(3, 4, {{3, 0, 1.0}}); });
	expect_canonical("identity(4)", CsrMatrix::identity(4));
}

void t_transpose()
{
	CsrMatrix a = random_matrix(7, 5, 1);
	Dense d = a.to_dense(), want(5, std::vector<double>(7, 0.0));
	for (std::size_t r = 0; r < 7; ++r)
		for (std::size_t c = 0; c < 5; ++c)
			want[c][r] = d[r][c];
	CsrMatrix t = a.transpose();
	expect_dense("transpose of a 7x5 matrix", t, want);
	if (t.transpose() != a)
		fail("transpose().transpose() differs from the original");
	if (a != random_matrix(7, 5, 1))
		fail("transpose() modified *this");
	CsrMatrix e = CsrMatrix(0, 3).transpose();
	if (e.rows() != 3 || e.cols() != 0 || e.nnz() != 0)
		fail("transpose of a 0x3 matrix: expected 3x0, got %s", shape(e).c_str());
	expect_canonical("transpose of a 0x3 matrix", e);
	CsrMatrix z = CsrMatrix::from_triplets(4, 6, {{3, 5, 2.0}, {3, 0, 1.0}}).transpose();
	expect_dense("transpose with empty rows", z,
		     {{0, 0, 0, 1}, {0, 0, 0, 0}, {0, 0, 0, 0}, {0, 0, 0, 0}, {0, 0, 0, 0}, {0, 0, 0, 2}});
}

void t_spmv()
{
	CsrMatrix a = random_matrix(6, 9, 2);
	Dense d = a.to_dense();
	std::vector<double> x(9);
	for (std::size_t i = 0; i < 9; ++i)
		x[i] = static_cast<double>(i) - 3.5;
	std::vector<double> y = a.multiply(x);
	if (y.size() != 6)
		fail("multiply(vector): expected 6 results, got %zu", y.size());
	for (std::size_t r = 0; r < 6; ++r) {
		double s = 0;
		for (std::size_t c = 0; c < 9; ++c)
			s += d[r][c] * x[c];
		if (y[r] != s)
			fail("multiply(vector): y[%zu] expected %g, got %g", r, s, y[r]);
	}
	std::vector<double> z = CsrMatrix(3, 2).multiply(std::vector<double>{1, 2});
	if (z != std::vector<double>{0, 0, 0})
		fail("multiply(vector) on an all-zero 3x2 matrix: expected three zeros");
	expect_throw<std::invalid_argument>("multiply(vector) with 8 values for 9 columns", "std::invalid_argument",
					    [&] { a.multiply(std::vector<double>(8, 1.0)); });
}

void t_spgemm()
{
	CsrMatrix a = random_matrix(5, 7, 3, 45), b = random_matrix(7, 4, 4, 45);
	expect_dense("5x7 times 7x4", a.multiply(b), dense_mul(a.to_dense(), b.to_dense(), 5, 7, 4));
	CsrMatrix wide = random_matrix(3, 12, 5, 60), tall = random_matrix(12, 10, 6, 60);
	expect_dense("3x12 times 12x10", wide.multiply(tall), dense_mul(wide.to_dense(), tall.to_dense(), 3, 12, 10));
	if (CsrMatrix::identity(5).multiply(a) != a || a.multiply(CsrMatrix::identity(7)) != a)
		fail("multiplying by the identity changed the matrix");
	CsrMatrix e = CsrMatrix(2, 0).multiply(CsrMatrix(0, 3));
	if (e.rows() != 2 || e.cols() != 3 || e.nnz() != 0)
		fail("2x0 times 0x3: expected an empty 2x3 matrix, got %s with %zu entries", shape(e).c_str(), e.nnz());
	expect_canonical("2x0 times 0x3", e);
	expect_throw<std::invalid_argument>("5x7 times 5x7", "std::invalid_argument", [&] { a.multiply(a); });
}

void t_spgemm_cancellation()
{
	CsrMatrix a = CsrMatrix::from_triplets(2, 2, {{0, 0, 1.0}, {0, 1, 1.0}, {1, 0, 1.0}, {1, 1, 2.0}});
	CsrMatrix b = CsrMatrix::from_triplets(2, 3, {{0, 0, 2.0}, {1, 0, -2.0}, {0, 2, 1.0}, {1, 1, 3.0}});
	CsrMatrix c = a.multiply(b);
	expect_dense("product with a cancelling entry", c, {{0, 3, 1}, {-2, 6, 1}});
	if (c.nnz() != 5)
		fail("product with a cancelling entry: expected 5 stored entries, got %zu", c.nnz());
	// columns reached in descending order inside a row must still come out sorted
	CsrMatrix p = CsrMatrix::from_triplets(1, 3, {{0, 0, 1.0}, {0, 1, 1.0}, {0, 2, 1.0}});
	CsrMatrix q = CsrMatrix::from_triplets(3, 4, {{0, 3, 1.0}, {1, 2, 1.0}, {2, 0, 1.0}, {2, 1, -1.0}, {1, 1, 1.0}});
	expect_dense("product with unsorted accumulation", p.multiply(q), {{1, 0, 1, 1}});
}

void t_add()
{
	CsrMatrix a = random_matrix(6, 5, 7), b = random_matrix(6, 5, 8);
	Dense da = a.to_dense(), db = b.to_dense(), want = da;
	for (std::size_t r = 0; r < 6; ++r)
		for (std::size_t c = 0; c < 5; ++c)
			want[r][c] = da[r][c] + 2.5 * db[r][c];
	expect_dense("A + 2.5 B", a.add(b, 2.5), want);
	for (std::size_t r = 0; r < 6; ++r)
		for (std::size_t c = 0; c < 5; ++c)
			want[r][c] = da[r][c] + db[r][c];
	expect_dense("A + B", a.add(b), want);
	CsrMatrix z = a.add(a, -1.0);
	if (z.rows() != 6 || z.cols() != 5 || z.nnz() != 0)
		fail("A - A: expected an empty 6x5 matrix, got %s with %zu entries", shape(z).c_str(), z.nnz());
	if (a.add(b, 0.0) != a)
		fail("A + 0 * B differs from A");
	expect_throw<std::invalid_argument>("adding a 6x5 and a 5x6 matrix", "std::invalid_argument",
					    [&] { a.add(a.transpose()); });
}

void t_prune()
{
	CsrMatrix a = CsrMatrix::from_triplets(2, 3, {{0, 0, 0.5}, {0, 2, -0.05}, {1, 1, 1e-9}, {1, 2, -2.0}});
	expect_dense("prune(0.05)", a.prune(0.05), {{0.5, 0, 0}, {0, 0, -2.0}});
	if (a.prune(0.0) != a)
		fail("prune(0) changed the matrix");
	if (a.prune(10.0).nnz() != 0)
		fail("prune(10): expected no entries left");
	if (a.nnz() != 4)
		fail("prune modified *this");
	expect_throw<std::invalid_argument>("prune(-1)", "std::invalid_argument", [&] { a.prune(-1.0); });
}

void t_diagonal()
{
	CsrMatrix a = CsrMatrix::from_triplets(3, 5, {{0, 0, 1.0}, {2, 2, 3.0}, {1, 4, 9.0}});
	if (a.diagonal() != std::vector<double>{1, 0, 3})
		fail("diagonal of a 3x5 matrix: expected [1, 0, 3]");
	if (a.transpose().diagonal() != std::vector<double>{1, 0, 3})
		fail("diagonal of a 5x3 matrix: expected [1, 0, 3]");
	if (!CsrMatrix(0, 4).diagonal().empty())
		fail("diagonal of a 0x4 matrix: expected no values");
}

void t_compose()
{
	CsrMatrix a = random_matrix(4, 6, 9, 50), b = random_matrix(6, 5, 10, 50);
	if (a.multiply(b).transpose() != b.transpose().multiply(a.transpose()))
		fail("(A B)^T differs from B^T A^T");
	CsrMatrix c = random_matrix(4, 6, 11, 50);
	std::vector<double> x{1, -2, 3, 0.5, -1, 2};
	std::vector<double> lhs = a.add(c).multiply(x), ax = a.multiply(x), cx = c.multiply(x);
	for (std::size_t i = 0; i < 4; ++i)
		if (lhs[i] != ax[i] + cx[i])
			fail("(A + C) x differs from A x + C x at %zu", i);
}

void t_mm_read_general()
{
	CsrMatrix m = mm_read("real general", "%%MatrixMarket matrix coordinate real general\n"
					      "% a comment\n"
					      "\n"
					      "3 4 4\n"
					      "1 1 1.5\n"
					      "3 4 -2e3\n"
					      "% comments may appear between entries\n"
					      "2 2   7\n"
					      "1 1 0.25\n");
	expect_dense("real general", m, {{1.75, 0, 0, 0}, {0, 7, 0, 0}, {0, 0, 0, -2000}});
	m = mm_read("integer, CRLF, mixed-case header", "%%matrixmarket MATRIX Coordinate Integer GENERAL\r\n"
							"2 2 3\r\n1 2 5\r\n2 1 -3\r\n2 2 3\r\n");
	expect_dense("integer, CRLF, mixed-case header", m, {{0, 5}, {-3, 3}});
	m = mm_read("pattern", "%%MatrixMarket matrix coordinate pattern general\n2 3 2\n1 3\n2 1\n");
	expect_dense("pattern", m, {{0, 0, 1}, {1, 0, 0}});
	m = mm_read("entries that cancel", "%%MatrixMarket matrix coordinate real general\n2 2 3\n1 1 2\n1 1 -2\n2 2 1\n");
	expect_dense("entries that cancel", m, {{0, 0}, {0, 1}});
	m = mm_read("no entries", "%%MatrixMarket matrix coordinate real general\n0 0 0\n");
	if (m.rows() != 0 || m.cols() != 0)
		fail("no entries: expected 0x0, got %s", shape(m).c_str());
}

void t_mm_read_symmetric()
{
	CsrMatrix s = mm_read("symmetric", "%%MatrixMarket matrix coordinate real symmetric\n"
					   "3 3 4\n1 1 2\n2 1 -1\n3 2 4\n3 3 5\n");
	expect_dense("symmetric", s, {{2, -1, 0}, {-1, 0, 4}, {0, 4, 5}});
	CsrMatrix k = mm_read("skew-symmetric", "%%MatrixMarket matrix coordinate integer skew-symmetric\n"
						"3 3 2\n2 1 3\n3 1 -1\n");
	expect_dense("skew-symmetric", k, {{0, -3, 1}, {3, 0, 0}, {-1, 0, 0}});
	CsrMatrix p = mm_read("pattern symmetric", "%%MatrixMarket matrix coordinate pattern symmetric\n2 2 2\n1 1\n2 1\n");
	expect_dense("pattern symmetric", p, {{1, 1}, {1, 0}});
}

void t_mm_read_errors()
{
	const std::string h = "%%MatrixMarket matrix coordinate real general\n";
	expect_parse_error("empty input", "", 1);
	expect_parse_error("wrong banner", "%MatrixMarket matrix coordinate real general\n1 1 0\n", 1);
	expect_parse_error("array format", "%%MatrixMarket matrix array real general\n1 1\n1\n", 1);
	expect_parse_error("complex field", "%%MatrixMarket matrix coordinate complex general\n1 1 0\n", 1);
	expect_parse_error("hermitian", "%%MatrixMarket matrix coordinate real hermitian\n1 1 0\n", 1);
	expect_parse_error("missing size line", h + "% only comments\n", 3);
	expect_parse_error("bad size line", h + "% c\n3 x 1\n1 1 1\n", 3);
	expect_parse_error("size line with 2 numbers", h + "3 3\n", 2);
	expect_parse_error("row index 0", h + "2 2 2\n1 1 1\n0 1 1\n", 4);
	expect_parse_error("column out of range", h + "2 2 1\n\n1 3 1\n", 4);
	expect_parse_error("missing value", h + "2 2 1\n1 1\n", 3);
	expect_parse_error("extra field", h + "2 2 1\n1 1 1 1\n", 3);
	expect_parse_error("value not a number", h + "2 2 1\n1 1 abc\n", 3);
	expect_parse_error("integer field with a fraction",
			   "%%MatrixMarket matrix coordinate integer general\n2 2 1\n1 1 1.5\n", 3);
	expect_parse_error("too few entries", h + "2 2 3\n1 1 1\n2 2 1\n", 5);
	expect_parse_error("too many entries", h + "2 2 1\n1 1 1\n2 2 1\n", 4);
	expect_parse_error("symmetric but not square",
			   "%%MatrixMarket matrix coordinate real symmetric\n2 3 0\n", 2);
	expect_parse_error("skew-symmetric diagonal",
			   "%%MatrixMarket matrix coordinate real skew-symmetric\n2 2 2\n2 1 1\n2 2 1\n", 4);
}

void t_mm_roundtrip()
{
	CsrMatrix a = CsrMatrix::from_triplets(3, 4, {{0, 1, 0.1}, {1, 3, 1e-300}, {2, 0, -123456.789},
						      {2, 3, 1.0 / 3.0}, {0, 3, 6.02214076e23}});
	std::ostringstream out;
	sparse::write_matrix_market(out, a);
	CsrMatrix b = mm_read("read back what write_matrix_market wrote", out.str());
	if (b != a)
		fail("write then read does not give the same matrix back");
	CsrMatrix r = random_matrix(9, 8, 12);
	std::ostringstream out2;
	sparse::write_matrix_market(out2, r);
	if (mm_read("round trip of a 9x8 matrix", out2.str()) != r)
		fail("round trip of a 9x8 matrix differs");
}

// ------------------------------------------------------------------ harness

std::size_t drain(int fd, char* buf, std::size_t cap)
{
	std::size_t len = 0;
	char tmp[4096];
	ssize_t n;
	while ((n = read(fd, tmp, sizeof tmp)) > 0) {
		std::size_t take = static_cast<std::size_t>(n);
		if (take > cap - 1 - len)
			take = cap - 1 - len;
		std::memcpy(buf + len, tmp, take);
		len += take;
	}
	buf[len] = '\0';
	return len;
}

std::string sanitizer_reason(const std::string& err)
{
	std::size_t e = err.find("ERROR: AddressSanitizer: ");
	std::size_t u = err.find("runtime error: ");
	std::string kind, where;
	if (e != std::string::npos) {
		std::size_t s = e + std::strlen("ERROR: AddressSanitizer: ");
		kind = "memory error (AddressSanitizer): " + err.substr(s, err.find_first_of(" \n", s) - s);
	} else if (u != std::string::npos) {
		std::size_t s = u + std::strlen("runtime error: ");
		kind = "undefined behaviour: " + err.substr(s, err.find('\n', s) - s);
	} else {
		return "";
	}
	// first frame in the library's own sources
	std::size_t pos = 0;
	while ((pos = err.find("\n    #", pos)) != std::string::npos) {
		std::size_t eol = err.find('\n', pos + 1);
		std::string fr = err.substr(pos + 1, eol == std::string::npos ? std::string::npos : eol - pos - 1);
		pos += 1;
		if (fr.find("__checker__") != std::string::npos || fr.find("libasan") != std::string::npos ||
		    fr.find("libubsan") != std::string::npos || fr.find(".cpp:") == std::string::npos ||
		    fr.find("/src/") == std::string::npos)
			continue;
		std::size_t in = fr.find(" in ");
		std::size_t slash = fr.rfind('/');
		if (in != std::string::npos && slash != std::string::npos) {
			std::size_t sp = fr.rfind(' ', slash);
			where = " in " + fr.substr(in + 4, sp - in - 4) + " (" + fr.substr(slash + 1) + ")";
			if (where.size() > 160)
				where = " at " + fr.substr(slash + 1);
		}
		break;
	}
	return kind + where;
}

void run(const char* name, void (*fn)())
{
	int mp[2], ep[2], st = 0;
	static char msg[600], err[65536];
	std::fflush(stdout);
	if (pipe(mp) || pipe(ep)) {
		std::printf("TEST %s FAIL could not create a pipe\n", name);
		return;
	}
	pid_t pid = fork();
	if (pid == 0) {
		close(mp[0]);
		close(ep[0]);
		dup2(ep[1], 2);
		msg_fd = mp[1];
		alarm(30);
		try {
			fn();
		} catch (const sparse::ParseError& e) {
			fail("unexpected sparse::ParseError: %s", e.what());
		} catch (const std::exception& e) {
			fail("unexpected exception %s: %s", tname(e).c_str(), e.what());
		} catch (...) {
			fail("unexpected non-std exception");
		}
		_exit(0);
	}
	close(mp[1]);
	close(ep[1]);
	drain(ep[0], err, sizeof err);
	drain(mp[0], msg, sizeof msg);
	close(mp[0]);
	close(ep[0]);
	waitpid(pid, &st, 0);
	if (WIFEXITED(st) && WEXITSTATUS(st) == 0) {
		std::printf("TEST %s PASS\n", name);
	} else if (msg[0]) {
		std::printf("TEST %s FAIL %s\n", name, msg);
	} else {
		std::string why = sanitizer_reason(err);
		if (!why.empty())
			std::printf("TEST %s FAIL %s\n", name, why.c_str());
		else if (WIFSIGNALED(st) && WTERMSIG(st) == SIGALRM)
			std::printf("TEST %s FAIL timed out after 30s\n", name);
		else if (WIFSIGNALED(st))
			std::printf("TEST %s FAIL crashed with signal %d\n", name, WTERMSIG(st));
		else
			std::printf("TEST %s FAIL exited with status %d\n", name, WEXITSTATUS(st));
	}
	std::fflush(stdout);
}

}  // namespace

int main()
{
	run("from_triplets", t_from_triplets);
	run("transpose", t_transpose);
	run("multiply_vector", t_spmv);
	run("multiply_matrix", t_spgemm);
	run("multiply_cancellation", t_spgemm_cancellation);
	run("add_scaled", t_add);
	run("prune", t_prune);
	run("diagonal", t_diagonal);
	run("operations_compose", t_compose);
	run("mm_read_general", t_mm_read_general);
	run("mm_read_symmetric", t_mm_read_symmetric);
	run("mm_read_errors", t_mm_read_errors);
	run("mm_roundtrip", t_mm_roundtrip);
	return 0;
}
