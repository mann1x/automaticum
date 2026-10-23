#include <stdexcept>

#include "sparse/csr.hpp"

namespace sparse {

CsrMatrix CsrMatrix::transpose() const
{
	throw std::logic_error("CsrMatrix::transpose: not implemented");
}

std::vector<double> CsrMatrix::multiply(const std::vector<double>& x) const
{
	(void)x;
	throw std::logic_error("CsrMatrix::multiply(vector): not implemented");
}

CsrMatrix CsrMatrix::multiply(const CsrMatrix& b) const
{
	(void)b;
	throw std::logic_error("CsrMatrix::multiply(matrix): not implemented");
}

CsrMatrix CsrMatrix::add(const CsrMatrix& b, double alpha) const
{
	(void)b;
	(void)alpha;
	throw std::logic_error("CsrMatrix::add: not implemented");
}

CsrMatrix CsrMatrix::prune(double tol) const
{
	(void)tol;
	throw std::logic_error("CsrMatrix::prune: not implemented");
}

std::vector<double> CsrMatrix::diagonal() const
{
	throw std::logic_error("CsrMatrix::diagonal: not implemented");
}

}  // namespace sparse
