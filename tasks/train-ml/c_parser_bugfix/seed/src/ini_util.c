#include <stdlib.h>
#include <string.h>

#include "ini_internal.h"

char *ini_strndup(const char *s, size_t n)
{
	char *p = malloc(n + 1);
	if (!p)
		return NULL;
	memcpy(p, s, n);
	p[n] = '\0';
	return p;
}

int ini_strcaseeq(const char *a, const char *b)
{
	for (;; a++, b++) {
		unsigned char x = (unsigned char)*a, y = (unsigned char)*b;
		if (x >= 'A' && x <= 'Z')
			x = (unsigned char)(x - 'A' + 'a');
		if (y >= 'A' && y <= 'Z')
			y = (unsigned char)(y - 'A' + 'a');
		if (x != y)
			return 0;
		if (x == '\0')
			return 1;
	}
}

int ini_is_blank(char c)
{
	return c == ' ' || c == '\t';
}

size_t ini_trim_len(const char *s, size_t n)
{
	while (n > 0 && ini_is_blank(s[n - 1]))
		n--;
	return n;
}

int ini_unescape(const char *in, size_t len, char *out, size_t outsz)
{
	size_t o = 0;

	if (outsz == 0)
		return INI_ERR_RANGE;
	for (size_t i = 0; i < len; i++) {
		char c = in[i];
		if (c == '\\') {
			if (++i >= len)
				return INI_ERR_SYNTAX;
			switch (in[i]) {
			case 'n':
				c = '\n';
				break;
			case 't':
				c = '\t';
				break;
			case '"':
				c = '"';
				break;
			case '\\':
				c = '\\';
				break;
			default:
				return INI_ERR_SYNTAX;
			}
		}
		if (o >= outsz)
			return INI_ERR_RANGE;
		out[o++] = c;
	}
	out[o] = '\0';
	return (int)o;
}

const char *ini_strerror(int code)
{
	switch (code) {
	case INI_OK:
		return "ok";
	case INI_ERR_SYNTAX:
		return "syntax error";
	case INI_ERR_NOMEM:
		return "out of memory";
	case INI_ERR_NOTFOUND:
		return "no such entry";
	case INI_ERR_TYPE:
		return "value has the wrong type";
	case INI_ERR_RANGE:
		return "value out of range";
	case INI_ERR_IO:
		return "i/o error";
	case INI_ERR_ARG:
		return "invalid argument";
	default:
		return "unknown error";
	}
}
