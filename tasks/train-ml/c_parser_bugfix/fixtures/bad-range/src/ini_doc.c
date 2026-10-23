#include <stdlib.h>
#include <string.h>

#include "ini_internal.h"

ini_doc *doc_new(void)
{
	return calloc(1, sizeof(ini_doc));
}

ini_entry *doc_find(const ini_doc *doc, const char *section, const char *key)
{
	for (size_t i = 0; i < doc->count; i++) {
		ini_entry *e = &doc->entries[i];
		if (ini_strcaseeq(e->section, section) && ini_strcaseeq(e->key, key))
			return e;
	}
	return NULL;
}

int doc_set(ini_doc *doc, const char *section, const char *key, const char *value, ini_entry **where)
{
	ini_entry *e = doc_find(doc, section, key);
	char *v = ini_strndup(value, strlen(value));

	if (!v)
		return INI_ERR_NOMEM;
	if (e) {
		free(e->value);
		e->value = v;
		*where = e;
		return INI_OK;
	}
	if (doc->count == doc->cap) {
		size_t ncap = doc->cap ? doc->cap * 2 : 8;
		ini_entry *n = realloc(doc->entries, ncap * sizeof *n);
		if (!n) {
			free(v);
			return INI_ERR_NOMEM;
		}
		doc->entries = n;
		doc->cap = ncap;
	}
	e = &doc->entries[doc->count];
	e->section = ini_strndup(section, strlen(section));
	e->key = ini_strndup(key, strlen(key));
	e->value = v;
	if (!e->section || !e->key) {
		free(e->section);
		free(e->key);
		free(v);
		return INI_ERR_NOMEM;
	}
	doc->count++;
	*where = e;
	return INI_OK;
}

int entry_append(ini_entry *e, const char *more)
{
	size_t a = strlen(e->value), b = strlen(more);
	char *nv = realloc(e->value, a + b + 2);

	if (!nv)
		return INI_ERR_NOMEM;
	nv[a] = ' ';
	memcpy(nv + a + 1, more, b + 1);
	e->value = nv;
	return INI_OK;
}

void ini_free(ini_doc *doc)
{
	if (!doc)
		return;
	for (size_t i = 0; i < doc->count; i++) {
		free(doc->entries[i].section);
		free(doc->entries[i].key);
		free(doc->entries[i].value);
	}
	free(doc->entries);
	free(doc);
}

size_t ini_count(const ini_doc *doc)
{
	return doc ? doc->count : 0;
}

void ini_foreach(const ini_doc *doc,
		 int (*fn)(const char *section, const char *key, const char *value, void *ctx),
		 void *ctx)
{
	if (!doc || !fn)
		return;
	for (size_t i = 0; i < doc->count; i++) {
		const ini_entry *e = &doc->entries[i];
		if (fn(e->section, e->key, e->value, ctx))
			return;
	}
}
