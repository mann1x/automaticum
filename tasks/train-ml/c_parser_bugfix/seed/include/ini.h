#ifndef INI_H
#define INI_H

#include <stddef.h>

enum {
	INI_OK = 0,
	INI_ERR_SYNTAX = -1,
	INI_ERR_NOMEM = -2,
	INI_ERR_NOTFOUND = -3,
	INI_ERR_TYPE = -4,
	INI_ERR_RANGE = -5,
	INI_ERR_IO = -6,
	INI_ERR_ARG = -7,
};

typedef struct ini_doc ini_doc;

int ini_parse_string(const char *text, ini_doc **out, int *err_line);
int ini_parse_file(const char *path, ini_doc **out, int *err_line);
void ini_free(ini_doc *doc);

const char *ini_get(const ini_doc *doc, const char *section, const char *key);
int ini_get_int(const ini_doc *doc, const char *section, const char *key, long *out);
size_t ini_count(const ini_doc *doc);

/* Visit every entry in file order; stops early when fn returns non-zero. */
void ini_foreach(const ini_doc *doc,
		 int (*fn)(const char *section, const char *key, const char *value, void *ctx),
		 void *ctx);

int ini_unescape(const char *in, size_t len, char *out, size_t outsz);
const char *ini_strerror(int code);

#endif
