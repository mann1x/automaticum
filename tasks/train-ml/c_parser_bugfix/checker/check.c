/* Hidden checks for libini. Each check runs in a forked child built with ASan/UBSan, so a memory
 * error in one check is reported as that check's failure and the others still run. */
#define _GNU_SOURCE
#include <limits.h>
#include <signal.h>
#include <stdarg.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/wait.h>
#include <unistd.h>

#include "ini.h"

static int msg_fd = -1;

static void fail(const char *fmt, ...)
{
	char buf[400];
	va_list ap;

	va_start(ap, fmt);
	vsnprintf(buf, sizeof buf, fmt, ap);
	va_end(ap);
	if (write(msg_fd, buf, strlen(buf)) < 0) {
	}
	_exit(1);
}

/* printable form of a string (escapes shown), rotating buffers */
static const char *show(const char *s)
{
	static char bufs[4][160];
	static int k;
	char *b = bufs[k++ & 3];
	size_t o = 0;

	if (!s)
		return "NULL";
	b[o++] = '"';
	for (; *s && o < 150; s++) {
		if (*s == '\n') {
			b[o++] = '\\';
			b[o++] = 'n';
		} else if (*s == '\t') {
			b[o++] = '\\';
			b[o++] = 't';
		} else if (*s == '"' || *s == '\\') {
			b[o++] = '\\';
			b[o++] = *s;
		} else {
			b[o++] = *s;
		}
	}
	if (*s) {
		memcpy(b + o, "...", 3);
		o += 3;
	}
	b[o++] = '"';
	b[o] = '\0';
	return b;
}

static ini_doc *parse_ok(const char *what, const char *text)
{
	ini_doc *doc = NULL;
	int line = -1;
	int rc = ini_parse_string(text, &doc, &line);

	if (rc != INI_OK)
		fail("%s: parse returned %d (%s) at line %d, expected INI_OK", what, rc, ini_strerror(rc), line);
	if (!doc)
		fail("%s: parse returned INI_OK but *out is NULL", what);
	if (line != 0)
		fail("%s: *err_line is %d after a successful parse, expected 0", what, line);
	return doc;
}

static void expect_str(const ini_doc *doc, const char *sec, const char *key, const char *want)
{
	const char *got = ini_get(doc, sec, key);

	if (!want && got)
		fail("[%s] %s: expected no entry, got %s", sec, key, show(got));
	if (want && (!got || strcmp(got, want) != 0))
		fail("[%s] %s: expected %s, got %s", sec, key, show(want), show(got));
}

static void expect_count(const ini_doc *doc, size_t want)
{
	if (ini_count(doc) != want)
		fail("ini_count: expected %zu, got %zu", want, ini_count(doc));
}

static void expect_syntax(const char *what, const char *text, int want_line)
{
	ini_doc *doc = (ini_doc *)&msg_fd;
	int line = -1;
	int rc = ini_parse_string(text, &doc, &line);

	if (rc != INI_ERR_SYNTAX)
		fail("%s: expected INI_ERR_SYNTAX, got %d (%s)", what, rc, ini_strerror(rc));
	if (line != want_line)
		fail("%s: expected error on line %d, got line %d", what, want_line, line);
	if (doc != NULL)
		fail("%s: *out must be NULL after an error", what);
}

static void expect_int(const ini_doc *doc, const char *key, int want_rc, long want)
{
	long v = 12345;
	int rc = ini_get_int(doc, "n", key, &v);

	if (rc != want_rc)
		fail("ini_get_int(%s = %s): expected %s, got %d (%s)", key, show(ini_get(doc, "n", key)),
		     ini_strerror(want_rc), rc, ini_strerror(rc));
	if (want_rc == INI_OK && v != want)
		fail("ini_get_int(%s = %s): expected %ld, got %ld", key, show(ini_get(doc, "n", key)), want, v);
	if (want_rc != INI_OK && v != 12345)
		fail("ini_get_int(%s = %s): *out was written on error (%ld)", key, show(ini_get(doc, "n", key)), v);
}

/* ---------------------------------------------------------------- checks */

static void t_basic_pairs(void)
{
	ini_doc *doc = parse_ok("basic", "name = demo\n[server]\nhost = example.org\nport=8080\n"
					 "[client]\nport = 9\n");
	expect_count(doc, 4);
	expect_str(doc, "", "name", "demo");
	expect_str(doc, "server", "host", "example.org");
	expect_str(doc, "SERVER", "Host", "example.org");
	expect_str(doc, "server", "port", "8080");
	expect_str(doc, "client", "port", "9");
	expect_str(doc, "server", "name", NULL);
	expect_str(doc, "nosuch", "port", NULL);
	ini_free(doc);
}

static void t_comments(void)
{
	ini_doc *doc = parse_ok("comments", "; top comment\n"
					    "  # indented comment\n"
					    "\n"
					    "[ main ]   ; section comment\n"
					    "a = 1 ; note\n"
					    "b = x;y\n"
					    "url = http://host/a#top\n"
					    "c = v\t# note\n"
					    "d = ; only a comment\n"
					    "e=#hash\n");
	expect_count(doc, 6);
	expect_str(doc, "main", "a", "1");
	expect_str(doc, "main", "b", "x;y");
	expect_str(doc, "main", "url", "http://host/a#top");
	expect_str(doc, "main", "c", "v");
	expect_str(doc, "main", "d", "");
	expect_str(doc, "main", "e", "");
	ini_free(doc);
}

static void t_line_endings(void)
{
	ini_doc *doc = parse_ok("crlf", "[w]\r\na = 1\r\nb=two words \r\n\r\nc = last");
	expect_count(doc, 3);
	expect_str(doc, "w", "a", "1");
	expect_str(doc, "w", "b", "two words");
	expect_str(doc, "w", "c", "last");
	ini_free(doc);
	doc = parse_ok("empty document", "");
	expect_count(doc, 0);
	ini_free(doc);
}

static void t_quoted_values(void)
{
	ini_doc *doc = parse_ok("quoted", "[q]\n"
					  "pad = \"  padded  \"\n"
					  "semi = \"semi ; colon # hash\" ; trailing comment\n"
					  "esc = \"tab\\there\\nnext\"\n"
					  "path = \"C:\\\\dir\\\\sub\"\n"
					  "empty = \"\"\n");
	expect_str(doc, "q", "pad", "  padded  ");
	expect_str(doc, "q", "semi", "semi ; colon # hash");
	expect_str(doc, "q", "esc", "tab\there\nnext");
	expect_str(doc, "q", "path", "C:\\dir\\sub");
	expect_str(doc, "q", "empty", "");
	ini_free(doc);
}

static void t_escaped_quotes(void)
{
	ini_doc *doc = parse_ok("escaped quotes", "[q]\n"
						  "say = \"she said \\\"hi\\\" twice\"\n"
						  "only = \"\\\"\"\n"
						  "dir = \"a\\\\\" ; ends with a backslash\n"
						  "mix = \"x\\\\\\\"y\"\n");
	expect_str(doc, "q", "say", "she said \"hi\" twice");
	expect_str(doc, "q", "only", "\"");
	expect_str(doc, "q", "dir", "a\\");
	expect_str(doc, "q", "mix", "x\\\"y");
	ini_free(doc);
}

static void t_syntax_errors(void)
{
	expect_syntax("line without '='", "[ok]\nx = 1\nnot a pair\n", 3);
	expect_syntax("unterminated section", "a=1\n[unterminated\n", 2);
	expect_syntax("empty section name", "[  ]\n", 1);
	expect_syntax("text after section", "[a] b\n", 1);
	expect_syntax("empty key", "[a]\n = v\n", 2);
	expect_syntax("unterminated quote", "[a]\nk = \"open\nz = 1\n", 2);
	expect_syntax("text after closing quote", "k = \"v\" junk\n", 1);
	expect_syntax("unknown escape", "\n\nk = \"bad \\q\"\n", 3);
	expect_syntax("crlf line count", "a=1\r\nb=2\r\noops\r\n", 3);
}

static void t_duplicates(void)
{
	ini_doc *doc = parse_ok("duplicates", "[s]\na = 1\nA = 2\n[t]\na = 3\n[S]\na = 4\nb = 5\n");
	expect_count(doc, 3);
	expect_str(doc, "s", "a", "4");
	expect_str(doc, "t", "a", "3");
	expect_str(doc, "s", "b", "5");
	ini_free(doc);
}

static void t_continuation(void)
{
	ini_doc *doc = parse_ok("continuation", "[d]\n"
						"desc = first\n"
						"  second part\n"
						"\tthird ; comment\n"
						"next = 1\n"
						"other = x\n"
						"\n"
						"  key2 = not a continuation\n"
						"q = \"quoted\"\n"
						"    more\n");
	expect_str(doc, "d", "desc", "first second part third");
	expect_str(doc, "d", "next", "1");
	expect_str(doc, "d", "other", "x");
	expect_str(doc, "d", "key2", "not a continuation");
	expect_str(doc, "d", "q", "quoted more");
	expect_count(doc, 5);
	ini_free(doc);
}

static void t_long_values(void)
{
	char *text = malloc(64 * 1024), *want = malloc(16 * 1024);
	size_t o = 0, w = 0;
	char key[16], val[16];

	o += (size_t)sprintf(text + o, "[big]\n");
	for (int i = 0; i < 40; i++)
		o += (size_t)sprintf(text + o, "k%d = v%d\n", i, i);
	o += (size_t)sprintf(text + o, "long = w0\n");
	w += (size_t)sprintf(want + w, "w0");
	for (int i = 1; i < 300; i++) {
		o += (size_t)sprintf(text + o, "   w%d\n", i);
		w += (size_t)sprintf(want + w, " w%d", i);
	}
	o += (size_t)sprintf(text + o, "after = done\n");
	ini_doc *doc = parse_ok("long document", text);
	expect_count(doc, 42);
	for (int i = 0; i < 40; i += 7) {
		sprintf(key, "k%d", i);
		sprintf(val, "v%d", i);
		expect_str(doc, "big", key, val);
	}
	const char *got = ini_get(doc, "big", "long");
	if (!got || strcmp(got, want) != 0)
		fail("value continued over 300 lines: expected %zu chars, got %s", strlen(want),
		     got ? "a different value" : "NULL");
	expect_str(doc, "big", "after", "done");
	ini_free(doc);
	free(text);
	free(want);
}

static void t_unescape(void)
{
	char *buf;
	int r;

	buf = malloc(4);
	r = ini_unescape("abc", 3, buf, 4);
	if (r != 3 || strcmp(buf, "abc") != 0)
		fail("ini_unescape(\"abc\", outsz 4): expected 3 \"abc\", got %d", r);
	free(buf);

	buf = malloc(3);
	r = ini_unescape("abc", 3, buf, 3);
	if (r != INI_ERR_RANGE)
		fail("ini_unescape(\"abc\", outsz 3): expected INI_ERR_RANGE (no room for the NUL), got %d", r);
	free(buf);

	buf = malloc(4);
	r = ini_unescape("a\\nb", 4, buf, 4);
	if (r != 3 || strcmp(buf, "a\nb") != 0)
		fail("ini_unescape(\"a\\\\nb\", outsz 4): expected 3, got %d", r);
	free(buf);

	buf = malloc(1);
	r = ini_unescape("", 0, buf, 1);
	if (r != 0 || buf[0] != '\0')
		fail("ini_unescape(\"\", outsz 1): expected 0 and an empty string, got %d", r);
	r = ini_unescape("x", 1, buf, 1);
	if (r != INI_ERR_RANGE)
		fail("ini_unescape(\"x\", outsz 1): expected INI_ERR_RANGE, got %d", r);
	free(buf);

	buf = malloc(8);
	r = ini_unescape("ab\\", 3, buf, 8);
	if (r != INI_ERR_SYNTAX)
		fail("ini_unescape with a trailing backslash: expected INI_ERR_SYNTAX, got %d", r);
	r = ini_unescape("\\x", 2, buf, 8);
	if (r != INI_ERR_SYNTAX)
		fail("ini_unescape(\"\\\\x\"): expected INI_ERR_SYNTAX, got %d", r);
	free(buf);
}

static void t_int_values(void)
{
	ini_doc *doc = parse_ok("ints", "[n]\na = 10\nb = -42\nc = +7\nd = 0x1F\ne = -0X10\n"
					"max = 9223372036854775807\nmin = -9223372036854775808\nz = 0\n");
	expect_int(doc, "a", INI_OK, 10);
	expect_int(doc, "b", INI_OK, -42);
	expect_int(doc, "c", INI_OK, 7);
	expect_int(doc, "d", INI_OK, 31);
	expect_int(doc, "e", INI_OK, -16);
	expect_int(doc, "max", INI_OK, LONG_MAX);
	expect_int(doc, "min", INI_OK, LONG_MIN);
	expect_int(doc, "z", INI_OK, 0);
	ini_free(doc);
}

static void t_int_errors(void)
{
	ini_doc *doc = parse_ok("int errors", "[n]\njunk = 12abc\nempty =\nword = abc\nsp = \" 5\"\nhex = 0x\n"
					      "sign = +-5\nbig = 9223372036854775808\nsmall = -9223372036854775809\n"
					      "bighex = 0x8000000000000000\nhuge = 123456789012345678901234567890\n");
	expect_int(doc, "missing", INI_ERR_NOTFOUND, 0);
	expect_int(doc, "junk", INI_ERR_TYPE, 0);
	expect_int(doc, "empty", INI_ERR_TYPE, 0);
	expect_int(doc, "word", INI_ERR_TYPE, 0);
	expect_int(doc, "sp", INI_ERR_TYPE, 0);
	expect_int(doc, "hex", INI_ERR_TYPE, 0);
	expect_int(doc, "sign", INI_ERR_TYPE, 0);
	expect_int(doc, "big", INI_ERR_RANGE, 0);
	expect_int(doc, "small", INI_ERR_RANGE, 0);
	expect_int(doc, "bighex", INI_ERR_RANGE, 0);
	expect_int(doc, "huge", INI_ERR_RANGE, 0);
	ini_free(doc);
}

static void t_parse_file(void)
{
	const char *dir = getenv("TMPDIR");
	char path[512];
	FILE *f;
	ini_doc *doc = NULL;
	int line = -1, rc;

	snprintf(path, sizeof path, "%s/libini-check.ini", dir ? dir : ".");
	f = fopen(path, "wb");
	if (!f)
		fail("could not create a temporary file");
	fputs("[file]\nk = from file\r\n  and more\n", f);
	fclose(f);
	rc = ini_parse_file(path, &doc, &line);
	if (rc != INI_OK)
		fail("ini_parse_file: expected INI_OK, got %d (%s)", rc, ini_strerror(rc));
	expect_str(doc, "file", "k", "from file and more");
	ini_free(doc);
	remove(path);
	doc = (ini_doc *)&msg_fd;
	rc = ini_parse_file(path, &doc, &line);
	if (rc != INI_ERR_IO || doc != NULL)
		fail("ini_parse_file on a missing file: expected INI_ERR_IO and NULL, got %d", rc);
}

/* ---------------------------------------------------------------- harness */

static size_t drain(int fd, char *buf, size_t cap)
{
	size_t len = 0;
	char tmp[4096];
	ssize_t n;

	while ((n = read(fd, tmp, sizeof tmp)) > 0) {
		size_t take = (size_t)n;
		if (take > cap - 1 - len)
			take = cap - 1 - len;
		memcpy(buf + len, tmp, take);
		len += take;
	}
	buf[len] = '\0';
	return len;
}

static const char *base_name(const char *p, const char *end)
{
	const char *b = p;
	for (const char *q = p; q < end; q++)
		if (*q == '/')
			b = q + 1;
	return b;
}

/* First stack frame after `from` that lies in the library (not the sanitizer runtime, not the
 * checks): "func (file.c:line)". */
static int lib_frame(const char *from, char *out, size_t cap)
{
	const char *l = from;

	while ((l = strstr(l, "\n    #")) != NULL) {
		const char *eol = strchr(l + 1, '\n');
		const char *in = strstr(l, " in ");
		l++;
		if (!eol)
			eol = l + strlen(l);
		if (!in || in > eol)
			continue;
		if (memmem(l, (size_t)(eol - l), "__checker__", 11) || memmem(l, (size_t)(eol - l), "libasan", 7) ||
		    memmem(l, (size_t)(eol - l), "libubsan", 8))
			continue;
		const char *fn = in + 4, *sp = memchr(fn, ' ', (size_t)(eol - fn));
		if (!sp || !memmem(sp, (size_t)(eol - sp), ".c:", 3))
			continue;
		const char *b = base_name(sp + 1, eol);
		snprintf(out, cap, "%.*s (%.*s)", (int)(sp - fn), fn, (int)(eol - b), b);
		return 1;
	}
	return 0;
}

/* Turn a sanitizer report into a one-line reason that names the library location. */
static void describe_crash(const char *err, char *out, size_t cap)
{
	const char *e = strstr(err, "ERROR: AddressSanitizer: ");
	const char *u = strstr(err, "runtime error: ");

	if (e) {
		const char *kind = e + strlen("ERROR: AddressSanitizer: ");
		size_t klen = strcspn(kind, " \n");
		char at[200] = "", freed[200] = "";
		const char *fr = strstr(e, "freed by thread");
		int has_at = lib_frame(e, at, sizeof at);
		int has_fr = fr && lib_frame(fr, freed, sizeof freed);
		snprintf(out, cap, "memory error (AddressSanitizer): %.*s%s%s%s%s", (int)klen, kind,
			 has_at ? " in " : "", has_at ? at : "", has_fr ? "; the memory was freed in " : "",
			 has_fr ? freed : "");
	} else if (u) {
		const char *ls = u;
		while (ls > err && ls[-1] != '\n')
			ls--;
		const char *eol = strchr(u, '\n');
		if (!eol)
			eol = u + strlen(u);
		const char *msg = u + strlen("runtime error: ");
		const char *b = base_name(ls, u);
		int in_checker = memmem(ls, (size_t)(u - ls), "__checker__", 11) != NULL;
		snprintf(out, cap, "undefined behaviour: %.*s%s%.*s", (int)(eol - msg), msg,
			 in_checker ? "" : " at ", in_checker ? 0 : (int)(u - 2 - b), b);
	} else {
		out[0] = '\0';
	}
}

static void run(const char *name, void (*fn)(void))
{
	int mp[2], ep[2], st;
	char msg[600], err[65536], why[500];
	pid_t pid;

	fflush(stdout);
	if (pipe(mp) || pipe(ep)) {
		printf("TEST %s FAIL could not create a pipe\n", name);
		return;
	}
	pid = fork();
	if (pid == 0) {
		close(mp[0]);
		close(ep[0]);
		dup2(ep[1], 2);
		msg_fd = mp[1];
		alarm(20);
		fn();
		_exit(0);
	}
	close(mp[1]);
	close(ep[1]);
	drain(ep[0], err, sizeof err);
	drain(mp[0], msg, sizeof msg);
	close(mp[0]);
	close(ep[0]);
	waitpid(pid, &st, 0);
	if (WIFEXITED(st) && WEXITSTATUS(st) == 0) {
		printf("TEST %s PASS\n", name);
	} else if (msg[0]) {
		printf("TEST %s FAIL %s\n", name, msg);
	} else {
		describe_crash(err, why, sizeof why);
		if (why[0])
			printf("TEST %s FAIL %s\n", name, why);
		else if (WIFSIGNALED(st) && WTERMSIG(st) == SIGALRM)
			printf("TEST %s FAIL timed out after 20s\n", name);
		else if (WIFSIGNALED(st))
			printf("TEST %s FAIL crashed with signal %d\n", name, WTERMSIG(st));
		else
			printf("TEST %s FAIL exited with status %d\n", name, WEXITSTATUS(st));
	}
	fflush(stdout);
}

int main(void)
{
	run("basic_pairs", t_basic_pairs);
	run("comments", t_comments);
	run("line_endings", t_line_endings);
	run("quoted_values", t_quoted_values);
	run("escaped_quotes", t_escaped_quotes);
	run("syntax_errors", t_syntax_errors);
	run("duplicate_keys", t_duplicates);
	run("continuation_lines", t_continuation);
	run("long_values", t_long_values);
	run("unescape_bounds", t_unescape);
	run("int_values", t_int_values);
	run("int_errors", t_int_errors);
	run("parse_file", t_parse_file);
	return 0;
}
