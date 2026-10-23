// spinfo: print the shape of a Matrix Market file, or a transformed copy of it.
//
//   spinfo FILE              rows, cols, nnz and the diagonal
//   spinfo FILE transpose    write FILE's transpose to stdout
//   spinfo FILE square       write FILE * FILE to stdout
#include <cstring>
#include <exception>
#include <iostream>

#include "sparse/csr.hpp"
#include "sparse/mm_io.hpp"

int main(int argc, char** argv)
{
	if (argc < 2 || argc > 3) {
		std::cerr << "usage: spinfo FILE [transpose|square]\n";
		return 2;
	}
	try {
		const sparse::CsrMatrix m = sparse::load_matrix_market(argv[1]);
		if (argc == 2) {
			std::cout << m.rows() << " x " << m.cols() << ", " << m.nnz() << " stored entries\n";
			std::cout << "diagonal:";
			for (double d : m.diagonal())
				std::cout << ' ' << d;
			std::cout << '\n';
		} else if (std::strcmp(argv[2], "transpose") == 0) {
			sparse::write_matrix_market(std::cout, m.transpose());
		} else if (std::strcmp(argv[2], "square") == 0) {
			sparse::write_matrix_market(std::cout, m.multiply(m));
		} else {
			std::cerr << "spinfo: unknown command " << argv[2] << '\n';
			return 2;
		}
	} catch (const std::exception& e) {
		std::cerr << "spinfo: " << e.what() << '\n';
		return 1;
	}
	return 0;
}
