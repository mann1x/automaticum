#include <stdio.h>
#include <string.h>

#include "ini.h"

static int failures;

#define EXPECT(cond)                                                          \
	do {                                                                  \
		if (!(cond)) {                                                \
			fprintf(stderr, "%s:%d: expected %s\n", __FILE__, __LINE__, #cond); \
			failures++;                                           \
		}                                                             \
	} while (0)

static void test_basic(void)
{
	ini_doc *doc;
	int line = -1;
	const char *text = "; app settings\n"
			   "name = demo\n"
			   "\n"
			   "[server]\n"
			   "host = example.org   ; primary\n"
			   "port=8080\n";

	EXPECT(ini_parse_string(text, &doc, &line) == INI_OK);
	EXPECT(line == 0);
	EXPECT(ini_count(doc) == 3);
	EXPECT(strcmp(ini_get(doc, "", "name"), "demo") == 0);
	EXPECT(strcmp(ini_get(doc, "server", "host"), "example.org") == 0);
	EXPECT(ini_get(doc, "server", "missing") == NULL);
	ini_free(doc);
}

static void test_ints(void)
{
	ini_doc *doc;
	long v = 0;

	EXPECT(ini_parse_string("[n]\na = 42\nb = -0x10\nc = 4x\n", &doc, NULL) == INI_OK);
	EXPECT(ini_get_int(doc, "n", "a", &v) == INI_OK && v == 42);
	EXPECT(ini_get_int(doc, "n", "b", &v) == INI_OK && v == -16);
	EXPECT(ini_get_int(doc, "n", "c", &v) == INI_ERR_TYPE);
	EXPECT(ini_get_int(doc, "n", "d", &v) == INI_ERR_NOTFOUND);
	ini_free(doc);
}

static void test_syntax_error(void)
{
	ini_doc *doc = (ini_doc *)&failures;
	int line = 0;

	EXPECT(ini_parse_string("[a]\nx = 1\nnot a pair\n", &doc, &line) == INI_ERR_SYNTAX);
	EXPECT(line == 3);
	EXPECT(doc == NULL);
}

int main(void)
{
	test_basic();
	test_ints();
	test_syntax_error();
	if (failures) {
		printf("%d check(s) failed\n", failures);
		return 1;
	}
	puts("all tests passed");
	return 0;
}
