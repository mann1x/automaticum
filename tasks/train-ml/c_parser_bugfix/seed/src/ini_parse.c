#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "ini_internal.h"

struct parser {
	ini_doc *doc;
	char *section;
	ini_entry *last; /* entry a continuation line would extend, or NULL */
};

/* Length of an unquoted value: an inline comment and the blanks before it are dropped. */
static size_t unquoted_len(const char *v, size_t n)
{
	for (size_t i = 0; i < n; i++) {
		if ((v[i] == ';' || v[i] == '#') && (i == 0 || ini_is_blank(v[i - 1]))) {
			n = i;
			break;
		}
	}
	return ini_trim_len(v, n);
}

static int parse_quoted(const char *q, size_t n, char **out)
{
	size_t i = 1, j;
	char *buf;
	int r;

	while (i < n && q[i] != '"')
		i++;
	if (i >= n)
		return INI_ERR_SYNTAX;
	for (j = i + 1; j < n && ini_is_blank(q[j]); j++)
		;
	if (j < n && q[j] != ';' && q[j] != '#')
		return INI_ERR_SYNTAX;
	buf = malloc(i);
	if (!buf)
		return INI_ERR_NOMEM;
	r = ini_unescape(q + 1, i - 1, buf, i);
	if (r < 0) {
		free(buf);
		return r;
	}
	*out = buf;
	return INI_OK;
}

static int parse_section(struct parser *p, const char *s, size_t n)
{
	const char *close = memchr(s, ']', n);
	const char *name, *rest;
	size_t len;
	char *copy;

	if (!close)
		return INI_ERR_SYNTAX;
	for (rest = close + 1; rest < s + n && ini_is_blank(*rest); rest++)
		;
	if (rest < s + n && *rest != ';' && *rest != '#')
		return INI_ERR_SYNTAX;
	for (name = s + 1; name < close && ini_is_blank(*name); name++)
		;
	len = ini_trim_len(name, (size_t)(close - name));
	if (len == 0)
		return INI_ERR_SYNTAX;
	copy = ini_strndup(name, len);
	if (!copy)
		return INI_ERR_NOMEM;
	free(p->section);
	p->section = copy;
	p->last = NULL;
	return INI_OK;
}

static int parse_pair(struct parser *p, const char *s, size_t n)
{
	const char *eq = memchr(s, '=', n);
	const char *v, *end = s + n;
	size_t klen;
	char *key, *value;
	int rc;

	if (!eq)
		return INI_ERR_SYNTAX;
	klen = ini_trim_len(s, (size_t)(eq - s));
	if (klen == 0)
		return INI_ERR_SYNTAX;
	for (v = eq + 1; v < end && ini_is_blank(*v); v++)
		;
	if (v < end && *v == '"') {
		rc = parse_quoted(v, (size_t)(end - v), &value);
		if (rc != INI_OK)
			return rc;
	} else {
		value = ini_strndup(v, unquoted_len(v, (size_t)(end - v)));
		if (!value)
			return INI_ERR_NOMEM;
	}
	key = ini_strndup(s, klen);
	if (!key) {
		free(value);
		return INI_ERR_NOMEM;
	}
	rc = doc_set(p->doc, p->section, key, value, &p->last);
	free(key);
	free(value);
	return rc;
}

static int parse_continuation(struct parser *p, const char *s, size_t n)
{
	char *more = ini_strndup(s, unquoted_len(s, n));
	int rc;

	if (!more)
		return INI_ERR_NOMEM;
	rc = entry_append(p->last, more);
	free(more);
	return rc;
}

static int parse_line(struct parser *p, const char *line, size_t n)
{
	const char *s = line;
	int indented = n > 0 && ini_is_blank(line[0]);
	size_t len;

	while (s < line + n && ini_is_blank(*s))
		s++;
	len = ini_trim_len(s, (size_t)(line + n - s));
	if (len == 0 || *s == ';' || *s == '#') {
		p->last = NULL;
		return INI_OK;
	}
	if (indented && p->last)
		return parse_continuation(p, s, len);
	if (*s == '[')
		return parse_section(p, s, len);
	return parse_pair(p, s, len);
}

int ini_parse_string(const char *text, ini_doc **out, int *err_line)
{
	struct parser p = {0};
	const char *cur = text;
	int line_no = 0, rc = INI_OK;

	if (err_line)
		*err_line = 0;
	if (!out)
		return INI_ERR_ARG;
	*out = NULL;
	if (!text)
		return INI_ERR_ARG;
	p.doc = doc_new();
	p.section = ini_strndup("", 0);
	if (!p.doc || !p.section) {
		ini_free(p.doc);
		free(p.section);
		return INI_ERR_NOMEM;
	}
	while (*cur) {
		const char *nl = strchr(cur, '\n');
		size_t n = nl ? (size_t)(nl - cur) : strlen(cur);
		const char *next = nl ? nl + 1 : cur + n;

		line_no++;
		if (n > 0 && cur[n - 1] == '\r')
			n--;
		rc = parse_line(&p, cur, n);
		if (rc != INI_OK)
			break;
		cur = next;
	}
	free(p.section);
	if (rc != INI_OK) {
		if (rc == INI_ERR_SYNTAX && err_line)
			*err_line = line_no;
		ini_free(p.doc);
		return rc;
	}
	*out = p.doc;
	return INI_OK;
}

int ini_parse_file(const char *path, ini_doc **out, int *err_line)
{
	FILE *f;
	char *buf = NULL;
	size_t len = 0, cap = 0;
	int rc;

	if (err_line)
		*err_line = 0;
	if (!out || !path)
		return INI_ERR_ARG;
	*out = NULL;
	f = fopen(path, "rb");
	if (!f)
		return INI_ERR_IO;
	for (;;) {
		if (cap - len < 4096) {
			char *nb = realloc(buf, cap + 8192);
			if (!nb) {
				free(buf);
				fclose(f);
				return INI_ERR_NOMEM;
			}
			buf = nb;
			cap += 8192;
		}
		size_t got = fread(buf + len, 1, cap - len - 1, f);
		len += got;
		if (got == 0)
			break;
	}
	rc = ferror(f) ? INI_ERR_IO : INI_OK;
	fclose(f);
	if (rc == INI_OK) {
		buf[len] = '\0';
		rc = ini_parse_string(buf, out, err_line);
	}
	free(buf);
	return rc;
}
