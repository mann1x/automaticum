#include "sparse/mm_io.hpp"

#include <cctype>
#include <cerrno>
#include <cstdio>
#include <cstdlib>
#include <fstream>
#include <istream>
#include <ostream>
#include <sstream>
#include <vector>

namespace sparse {

ParseError::ParseError(std::size_t line, const std::string& what)
    : std::runtime_error("line " + std::to_string(line) + ": " + what), line_(line)
{
}

namespace {

std::string lower(std::string s)
{
	for (char& ch : s)
		ch = static_cast<char>(std::tolower(static_cast<unsigned char>(ch)));
	return s;
}

std::vector<std::string> split(const std::string& line)
{
	std::istringstream in(line);
	std::vector<std::string> words;
	std::string w;
	while (in >> w)
		words.push_back(w);
	return words;
}

bool parse_count(const std::string& s, std::size_t& out)
{
	if (s.empty())
		return false;
	for (char ch : s)
		if (!std::isdigit(static_cast<unsigned char>(ch)))
			return false;
	errno = 0;
	const unsigned long long v = std::strtoull(s.c_str(), nullptr, 10);
	if (errno == ERANGE)
		return false;
	out = static_cast<std::size_t>(v);
	return true;
}

bool parse_value(const std::string& s, bool integer, double& out)
{
	if (integer) {
		std::size_t start = (s[0] == '+' || s[0] == '-') ? 1 : 0;
		if (start == s.size())
			return false;
		for (std::size_t i = start; i < s.size(); ++i)
			if (!std::isdigit(static_cast<unsigned char>(s[i])))
				return false;
	}
	char* end = nullptr;
	out = std::strtod(s.c_str(), &end);
	return end && *end == '\0' && end != s.c_str();
}

struct Lines {
	std::istream& in;
	std::size_t number = 0;

	// next line that is not blank and not a comment; false at end of input
	bool next(std::string& line)
	{
		while (std::getline(in, line)) {
			++number;
			if (!line.empty() && line.back() == '\r')
				line.pop_back();
			std::size_t i = line.find_first_not_of(" \t");
			if (i == std::string::npos || line[0] == '%')
				continue;
			return true;
		}
		return false;
	}
};

}  // namespace

CsrMatrix read_matrix_market(std::istream& in)
{
	Lines lines{in};
	std::string line;
	if (!std::getline(in, line))
		throw ParseError(1, "empty input");
	lines.number = 1;
	if (!line.empty() && line.back() == '\r')
		line.pop_back();
	const std::vector<std::string> head = split(line);
	if (head.size() != 5 || lower(head[0]) != "%%matrixmarket" || lower(head[1]) != "matrix")
		throw ParseError(1, "not a Matrix Market header");
	if (lower(head[2]) != "coordinate")
		throw ParseError(1, "only the coordinate format is supported");
	const std::string field = lower(head[3]);
	const std::string symmetry = lower(head[4]);
	if (field != "real" && field != "integer" && field != "pattern")
		throw ParseError(1, "unsupported field '" + head[3] + "'");
	if (symmetry != "general" && symmetry != "symmetric" && symmetry != "skew-symmetric")
		throw ParseError(1, "unsupported symmetry '" + head[4] + "'");

	if (!lines.next(line))
		throw ParseError(lines.number + 1, "missing size line");
	const std::vector<std::string> size = split(line);
	std::size_t rows = 0, cols = 0, nnz = 0;
	if (size.size() != 3 || !parse_count(size[0], rows) || !parse_count(size[1], cols) ||
	    !parse_count(size[2], nnz))
		throw ParseError(lines.number, "bad size line");
	if (symmetry != "general" && rows != cols)
		throw ParseError(lines.number, symmetry + " matrix must be square");

	const bool pattern = field == "pattern";
	std::vector<Triplet> entries;
	for (std::size_t n = 0; n < nnz; ++n) {
		if (!lines.next(line))
			throw ParseError(lines.number + 1, "expected " + std::to_string(nnz) + " entries, found " +
								   std::to_string(n));
		const std::vector<std::string> tok = split(line);
		if (tok.size() != (pattern ? 2u : 3u))
			throw ParseError(lines.number, "wrong number of fields");
		std::size_t i = 0, j = 0;
		double v = 1.0;
		if (!parse_count(tok[0], i) || !parse_count(tok[1], j))
			throw ParseError(lines.number, "bad index");
		if (i < 1 || i > rows || j < 1 || j > cols)
			throw ParseError(lines.number, "index out of range");
		if (!pattern && !parse_value(tok[2], field == "integer", v))
			throw ParseError(lines.number, "bad value '" + tok[2] + "'");
		entries.push_back({i - 1, j - 1, v});
		if (i != j) {
			if (symmetry == "symmetric")
				entries.push_back({j - 1, i - 1, v});
			else if (symmetry == "skew-symmetric")
				entries.push_back({j - 1, i - 1, -v});
		} else if (symmetry == "skew-symmetric") {
			throw ParseError(lines.number, "diagonal entry in a skew-symmetric matrix");
		}
	}
	if (lines.next(line))
		throw ParseError(lines.number, "more entries than declared");
	return CsrMatrix::from_triplets(rows, cols, std::move(entries));
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
