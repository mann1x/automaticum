#ifndef INI_INTERNAL_H
#define INI_INTERNAL_H

#include "ini.h"

typedef struct {
	char *section;
	char *key;
	char *value;
} ini_entry;

struct ini_doc {
	ini_entry *entries;
	size_t count;
	size_t cap;
};

ini_doc *doc_new(void);
ini_entry *doc_find(const ini_doc *doc, const char *section, const char *key);
int doc_set(ini_doc *doc, const char *section, const char *key, const char *value, ini_entry **where);
int entry_append(ini_entry *e, const char *more);

char *ini_strndup(const char *s, size_t n);
int ini_strcaseeq(const char *a, const char *b);
int ini_is_blank(char c);
size_t ini_trim_len(const char *s, size_t n);

#endif
