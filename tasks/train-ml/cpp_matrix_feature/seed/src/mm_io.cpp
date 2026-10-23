#include "sparse/mm_io.hpp"

#include <cstdio>
#include <fstream>
#include <istream>
#include <ostream>

namespace sparse {

ParseError::ParseError(std::size_t line, const std::string& what)
    : std::runtime_error("line " + std::to_string(line) + ": " + what), line_(line)
{
}

CsrMatrix read_matrix_market(std::istream& in)
{
	(void)in;
	throw std::logic_error("read_matrix_market: not implemented");
}

void write_matrix_market(std::ostream& out, const CsrMatrix& m)
{
	out << "%%MatrixMarket matrix coordinate real general\n";
	out << m.rows() << ' ' << m.cols() << ' ' << m.nnz() << '\n';
	char buf[64];
	for (const Triplet& t : m.triplets()) {
		std::snprintf(buf, sizeof buf, "%.17g", t.value);
		out << t.row + 1 << ' ' << t.col + 1 << ' ' << buf << '\n';
	}
}

CsrMatrix load_matrix_market(const std::string& path)
{
	std::ifstream in(path);
	if (!in)
		throw std::runtime_error("cannot open " + path);
	return read_matrix_market(in);
}

void save_matrix_market(const std::string& path, const CsrMatrix& m)
{
	std::ofstream out(path);
	if (!out)
		throw std::runtime_error("cannot open " + path + " for writing");
	write_matrix_market(out, m);
	if (!out)
		throw std::runtime_error("error writing " + path);
}

}  // namespace sparse
