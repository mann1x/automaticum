#include <stdio.h>

#include "ini.h"

static int print_entry(const char *section, const char *key, const char *value, void *ctx)
{
	(void)ctx;
	if (section[0])
		printf("%s.%s=%s\n", section, key, value);
	else
		printf("%s=%s\n", key, value);
	return 0;
}

int main(int argc, char **argv)
{
	ini_doc *doc;
	int line, rc;

	if (argc != 2 && argc != 4) {
		fprintf(stderr, "usage: %s FILE [SECTION KEY]\n", argv[0]);
		return 2;
	}
	rc = ini_parse_file(argv[1], &doc, &line);
	if (rc != INI_OK) {
		if (rc == INI_ERR_SYNTAX)
			fprintf(stderr, "%s:%d: %s\n", argv[1], line, ini_strerror(rc));
		else
			fprintf(stderr, "%s: %s\n", argv[1], ini_strerror(rc));
		return 1;
	}
	if (argc == 4) {
		const char *v = ini_get(doc, argv[2], argv[3]);
		if (!v) {
			fprintf(stderr, "%s: no key %s in [%s]\n", argv[1], argv[3], argv[2]);
			ini_free(doc);
			return 1;
		}
		puts(v);
	} else {
		ini_foreach(doc, print_entry, NULL);
	}
	ini_free(doc);
	return 0;
}
