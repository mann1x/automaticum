CC ?= gcc
CFLAGS ?= -std=c11 -Wall -Wextra -O2 -g
CPPFLAGS += -Iinclude -Isrc
AR ?= ar

SRC := $(wildcard src/*.c)
OBJ := $(SRC:src/%.c=build/%.o)

.PHONY: all test clean

all: libini.a inidump

libini.a: $(OBJ)
	$(AR) rcs $@ $^

build/%.o: src/%.c src/ini_internal.h include/ini.h | build
	$(CC) $(CPPFLAGS) $(CFLAGS) -c $< -o $@

build:
	mkdir -p build

inidump: tools/inidump.c libini.a
	$(CC) $(CPPFLAGS) $(CFLAGS) $< -L. -lini -o $@

tests/test_ini: tests/test_ini.c libini.a
	$(CC) $(CPPFLAGS) $(CFLAGS) $< -L. -lini -o $@

test: tests/test_ini
	./tests/test_ini

clean:
	rm -rf build libini.a inidump tests/test_ini
