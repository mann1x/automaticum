#include <algorithm>
#include <cmath>
#include <stdexcept>
#include <string>

#include "sparse/csr.hpp"

namespace sparse {

namespace {

std::string shape(std::size_t r, std::size_t c)
{
	return std::to_string(r) + "x" + std::to_string(c);
}

}  // namespace

CsrMatrix CsrMatrix::transpose() const
{
	CsrMatrix t(cols_, rows_);
	t.col_idx_.resize(nnz());
	t.values_.resize(nnz());
	for (std::size_t k = 0; k < nnz(); ++k)
		++t.row_ptr_[col_idx_[k] + 1];
	for (std::size_t c = 0; c < cols_; ++c)
		t.row_ptr_[c + 1] += t.row_ptr_[c];
	std::vector<std::size_t> next(t.row_ptr_.begin(), t.row_ptr_.end() - 1);
	for (std::size_t r = 0; r < rows_; ++r) {
		for (std::size_t k = row_ptr_[r]; k < row_ptr_[r + 1]; ++k) {
			const std::size_t dst = next[col_idx_[k]]++;
			t.col_idx_[dst] = r;
			t.values_[dst] = values_[k];
		}
	}
	return t;
}

std::vector<double> CsrMatrix::multiply(const std::vector<double>& x) const
{
	if (x.size() != cols_)
		throw std::invalid_argument("multiply: vector of size " + std::to_string(x.size()) + " for a " +
					    shape(rows_, cols_) + " matrix");
	std::vector<double> y(rows_, 0.0);
	for (std::size_t r = 0; r < rows_; ++r) {
		double s = 0.0;
		for (std::size_t k = row_ptr_[r]; k < row_ptr_[r + 1]; ++k)
			s += values_[k] * x[col_idx_[k]];
		y[r] = s;
	}
	return y;
}

CsrMatrix CsrMatrix::multiply(const CsrMatrix& b) const
{
	if (cols_ != b.rows_)
		throw std::invalid_argument("multiply: " + shape(rows_, cols_) + " by " + shape(b.rows_, b.cols_));
	CsrMatrix c(rows_, b.cols_);
	std::vector<double> acc(b.cols_, 0.0);
	std::vector<char> used(b.cols_, 0);
	std::vector<std::size_t> touched;
	for (std::size_t r = 0; r < rows_; ++r) {
		touched.clear();
		for (std::size_t k = row_ptr_[r]; k < row_ptr_[r + 1]; ++k) {
			const std::size_t mid = col_idx_[k];
			const double a = values_[k];
			for (std::size_t q = b.row_ptr_[mid]; q < b.row_ptr_[mid + 1]; ++q) {
				const std::size_t col = b.col_idx_[q];
				if (!used[col]) {
					used[col] = 1;
					touched.push_back(col);
				}
				acc[col] += a * b.values_[q];
			}
		}
		std::sort(touched.begin(), touched.end());
		for (std::size_t col : touched) {
			if (acc[col] != 0.0) {
				c.col_idx_.push_back(col);
				c.values_.push_back(acc[col]);
			}
			acc[col] = 0.0;
			used[col] = 0;
		}
		c.row_ptr_[r + 1] = c.col_idx_.size();
	}
	return c;
}

CsrMatrix CsrMatrix::add(const CsrMatrix& b, double alpha) const
{
	if (rows_ != b.rows_ || cols_ != b.cols_)
		throw std::invalid_argument("add: " + shape(rows_, cols_) + " and " + shape(b.rows_, b.cols_));
	CsrMatrix c(rows_, cols_);
	for (std::size_t r = 0; r < rows_; ++r) {
		std::size_t i = row_ptr_[r], j = b.row_ptr_[r];
		const std::size_t ie = row_ptr_[r + 1], je = b.row_ptr_[r + 1];
		while (i < ie || j < je) {
			std::size_t col;
			double v;
			if (j >= je || (i < ie && col_idx_[i] < b.col_idx_[j])) {
				col = col_idx_[i];
				v = values_[i++];
			} else if (i >= ie || b.col_idx_[j] < col_idx_[i]) {
				col = b.col_idx_[j];
				v = alpha * b.values_[j++];
			} else {
				col = col_idx_[i];
				v = values_[i++] + alpha * b.values_[j++];
			}
			if (v != 0.0) {
				c.col_idx_.push_back(col);
				c.values_.push_back(v);
			}
		}
		c.row_ptr_[r + 1] = c.col_idx_.size();
	}
	return c;
}

CsrMatrix CsrMatrix::prune(double tol) const
{
	if (tol < 0.0)
		throw std::invalid_argument("prune: negative tolerance");
	CsrMatrix c(rows_, cols_);
	for (std::size_t r = 0; r < rows_; ++r) {
		for (std::size_t k = row_ptr_[r]; k < row_ptr_[r + 1]; ++k) {
			if (std::fabs(values_[k]) > tol) {
				c.col_idx_.push_back(col_idx_[k]);
				c.values_.push_back(values_[k]);
			}
		}
		c.row_ptr_[r + 1] = c.col_idx_.size();
	}
	return c;
}

std::vector<double> CsrMatrix::diagonal() const
{
	const std::size_t n = std::min(rows_, cols_);
	std::vector<double> d(n, 0.0);
	for (std::size_t i = 0; i < n; ++i)
		d[i] = at(i, i);
	return d;
}

}  // namespace sparse
