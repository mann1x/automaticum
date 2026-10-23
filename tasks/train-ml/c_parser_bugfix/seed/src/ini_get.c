#include <ctype.h>
#include <errno.h>
#include <stdlib.h>

#include "ini_internal.h"

const char *ini_get(const ini_doc *doc, const char *section, const char *key)
{
	if (!doc || !section || !key)
		return NULL;
	ini_entry *e = doc_find(doc, section, key);
	return e ? e->value : NULL;
}

int ini_get_int(const ini_doc *doc, const char *section, const char *key, long *out)
{
	const char *v = ini_get(doc, section, key);
	const char *digits;
	char *end;
	int base = 10;
	long r;

	if (!v)
		return INI_ERR_NOTFOUND;
	digits = v;
	if (*digits == '+' || *digits == '-')
		digits++;
	if (digits[0] == '0' && (digits[1] == 'x' || digits[1] == 'X')) {
		base = 16;
		if (!isxdigit((unsigned char)digits[2]))
			return INI_ERR_TYPE;
	} else if (!isdigit((unsigned char)digits[0])) {
		return INI_ERR_TYPE;
	}
	errno = 0;
	r = strtol(v, &end, base);
	if (*end != '\0')
		return INI_ERR_TYPE;
	*out = r;
	return INI_OK;
}
