#include <cstdio>
#include <sstream>
#include <stdexcept>

#include "sparse/csr.hpp"
#include "sparse/mm_io.hpp"

using sparse::CsrMatrix;

static int failures = 0;

#define EXPECT(cond)                                                                      \
	do {                                                                              \
		if (!(cond)) {                                                            \
			std::fprintf(stderr, "%s:%d: expected %s\n", __FILE__, __LINE__, #cond); \
			++failures;                                                       \
		}                                                                         \
	} while (0)

int main()
{
	CsrMatrix m = CsrMatrix::from_triplets(2, 3, {{1, 2, 4.0}, {0, 0, 1.0}, {1, 2, 1.0}, {0, 1, 0.0}});
	EXPECT(m.rows() == 2 && m.cols() == 3);
	EXPECT(m.nnz() == 2);
	EXPECT(m.at(1, 2) == 5.0);
	EXPECT(m.at(0, 1) == 0.0);
	EXPECT(CsrMatrix::identity(3).nnz() == 3);

	bool threw = false;
	try {
		(void)m.at(2, 0);
	} catch (const std::out_of_range&) {
		threw = true;
	}
	EXPECT(threw);

	std::ostringstream out;
	sparse::write_matrix_market(out, m);
	EXPECT(out.str() == "%%MatrixMarket matrix coordinate real general\n2 3 2\n1 1 1\n2 3 5\n");

	if (failures) {
		std::printf("%d check(s) failed\n", failures);
		return 1;
	}
	std::puts("smoke tests passed");
	return 0;
}
