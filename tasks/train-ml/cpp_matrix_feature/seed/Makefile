CXX = g++
CXXFLAGS ?= -std=c++17 -Wall -Wextra -O2 -g
CPPFLAGS += -Iinclude
AR ?= ar

SRC := $(wildcard src/*.cpp)
OBJ := $(SRC:src/%.cpp=build/%.o)
HDR := $(wildcard include/sparse/*.hpp)

.PHONY: all test clean

all: libsparse.a spinfo

libsparse.a: $(OBJ)
	$(AR) rcs $@ $^

build/%.o: src/%.cpp $(HDR) | build
	$(CXX) $(CPPFLAGS) $(CXXFLAGS) -c $< -o $@

build:
	mkdir -p build

spinfo: tools/spinfo.cpp libsparse.a
	$(CXX) $(CPPFLAGS) $(CXXFLAGS) $< -L. -lsparse -o $@

tests/smoke: tests/smoke.cpp libsparse.a
	$(CXX) $(CPPFLAGS) $(CXXFLAGS) $< -L. -lsparse -o $@

test: tests/smoke
	./tests/smoke

clean:
	rm -rf build libsparse.a spinfo tests/smoke
