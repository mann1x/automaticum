#pragma once

#include <cstddef>
#include <iosfwd>
#include <stdexcept>
#include <string>

#include "sparse/csr.hpp"

namespace sparse {

// Thrown for malformed Matrix Market input. line() is 1-based.
class ParseError : public std::runtime_error {
public:
	ParseError(std::size_t line, const std::string& what);
	std::size_t line() const noexcept { return line_; }

private:
	std::size_t line_;
};

CsrMatrix read_matrix_market(std::istream& in);
void write_matrix_market(std::ostream& out, const CsrMatrix& m);

// Convenience wrappers around the stream functions; throw std::runtime_error if the file cannot be
// opened.
CsrMatrix load_matrix_market(const std::string& path);
void save_matrix_market(const std::string& path, const CsrMatrix& m);

}  // namespace sparse
