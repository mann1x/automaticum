#pragma once

#include <cstddef>
#include <vector>

namespace sparse {

struct Triplet {
	std::size_t row;
	std::size_t col;
	double value;
};

// A sparse matrix in compressed sparse row (CSR) form. See README.md for the invariants every
// CsrMatrix keeps.
class CsrMatrix {
public:
	CsrMatrix() = default;
	CsrMatrix(std::size_t rows, std::size_t cols);

	static CsrMatrix from_triplets(std::size_t rows, std::size_t cols, std::vector<Triplet> entries);
	static CsrMatrix identity(std::size_t n);

	std::size_t rows() const { return rows_; }
	std::size_t cols() const { return cols_; }
	std::size_t nnz() const { return values_.size(); }

	const std::vector<std::size_t>& row_ptr() const { return row_ptr_; }
	const std::vector<std::size_t>& col_idx() const { return col_idx_; }
	const std::vector<double>& values() const { return values_; }

	double at(std::size_t r, std::size_t c) const;
	std::vector<std::vector<double>> to_dense() const;
	std::vector<Triplet> triplets() const;

	bool operator==(const CsrMatrix& other) const;
	bool operator!=(const CsrMatrix& other) const { return !(*this == other); }

	CsrMatrix transpose() const;
	std::vector<double> multiply(const std::vector<double>& x) const;
	CsrMatrix multiply(const CsrMatrix& b) const;
	CsrMatrix add(const CsrMatrix& b, double alpha = 1.0) const;
	CsrMatrix prune(double tol) const;
	std::vector<double> diagonal() const;

private:
	std::size_t rows_ = 0;
	std::size_t cols_ = 0;
	std::vector<std::size_t> row_ptr_{0};
	std::vector<std::size_t> col_idx_;
	std::vector<double> values_;
};

}  // namespace sparse
