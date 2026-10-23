#include "sparse/csr.hpp"

#include <algorithm>
#include <stdexcept>
#include <string>

namespace sparse {

CsrMatrix::CsrMatrix(std::size_t rows, std::size_t cols) : rows_(rows), cols_(cols), row_ptr_(rows + 1, 0) {}

CsrMatrix CsrMatrix::from_triplets(std::size_t rows, std::size_t cols, std::vector<Triplet> entries)
{
	for (const Triplet& t : entries) {
		if (t.row >= rows || t.col >= cols)
			throw std::out_of_range("triplet (" + std::to_string(t.row) + ", " + std::to_string(t.col) +
						") is outside a " + std::to_string(rows) + "x" + std::to_string(cols) +
						" matrix");
	}
	std::stable_sort(entries.begin(), entries.end(), [](const Triplet& a, const Triplet& b) {
		return a.row < b.row || (a.row == b.row && a.col < b.col);
	});

	CsrMatrix m(rows, cols);
	std::size_t i = 0;
	for (std::size_t r = 0; r < rows; ++r) {
		while (i < entries.size() && entries[i].row == r) {
			const std::size_t c = entries[i].col;
			double sum = 0.0;
			while (i < entries.size() && entries[i].row == r && entries[i].col == c)
				sum += entries[i++].value;
			if (sum != 0.0) {
				m.col_idx_.push_back(c);
				m.values_.push_back(sum);
			}
		}
		m.row_ptr_[r + 1] = m.col_idx_.size();
	}
	return m;
}

CsrMatrix CsrMatrix::identity(std::size_t n)
{
	std::vector<Triplet> t;
	t.reserve(n);
	for (std::size_t i = 0; i < n; ++i)
		t.push_back({i, i, 1.0});
	return from_triplets(n, n, std::move(t));
}

double CsrMatrix::at(std::size_t r, std::size_t c) const
{
	if (r >= rows_ || c >= cols_)
		throw std::out_of_range("at(" + std::to_string(r) + ", " + std::to_string(c) + ") on a " +
					std::to_string(rows_) + "x" + std::to_string(cols_) + " matrix");
	const auto begin = col_idx_.begin() + static_cast<std::ptrdiff_t>(row_ptr_[r]);
	const auto end = col_idx_.begin() + static_cast<std::ptrdiff_t>(row_ptr_[r + 1]);
	const auto it = std::lower_bound(begin, end, c);
	if (it == end || *it != c)
		return 0.0;
	return values_[static_cast<std::size_t>(it - col_idx_.begin())];
}

std::vector<std::vector<double>> CsrMatrix::to_dense() const
{
	std::vector<std::vector<double>> d(rows_, std::vector<double>(cols_, 0.0));
	for (std::size_t r = 0; r < rows_; ++r)
		for (std::size_t k = row_ptr_[r]; k < row_ptr_[r + 1]; ++k)
			d[r][col_idx_[k]] = values_[k];
	return d;
}

std::vector<Triplet> CsrMatrix::triplets() const
{
	std::vector<Triplet> t;
	t.reserve(nnz());
	for (std::size_t r = 0; r < rows_; ++r)
		for (std::size_t k = row_ptr_[r]; k < row_ptr_[r + 1]; ++k)
			t.push_back({r, col_idx_[k], values_[k]});
	return t;
}

bool CsrMatrix::operator==(const CsrMatrix& other) const
{
	return rows_ == other.rows_ && cols_ == other.cols_ && row_ptr_ == other.row_ptr_ &&
	       col_idx_ == other.col_idx_ && values_ == other.values_;
}

}  // namespace sparse
