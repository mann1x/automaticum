const SPEC = {
 "env": {
  "ASAN_OPTIONS": "detect_leaks=0:abort_on_error=0",
  "UBSAN_OPTIONS": "print_stacktrace=0"
 },
 "files": {
  "__checker__/check.c": "/* Hidden checks for libini. Each check runs in a forked child built with ASan/UBSan, so a memory\n * error in one check is reported as that check's failure and the others still run. */\n#define _GNU_SOURCE\n#include <limits.h>\n#include <signal.h>\n#include <stdarg.h>\n#include <stdio.h>\n#include <stdlib.h>\n#include <string.h>\n#include <sys/wait.h>\n#include <unistd.h>\n\n#include \"ini.h\"\n\nstatic int msg_fd = -1;\n\nstatic void fail(const char *fmt, ...)\n{\n\tchar buf[400];\n\tva_list ap;\n\n\tva_start(ap, fmt);\n\tvsnprintf(buf, sizeof buf, fmt, ap);\n\tva_end(ap);\n\tif (write(msg_fd, buf, strlen(buf)) < 0) {\n\t}\n\t_exit(1);\n}\n\n/* printable form of a string (escapes shown), rotating buffers */\nstatic const char *show(const char *s)\n{\n\tstatic char bufs[4][160];\n\tstatic int k;\n\tchar *b = bufs[k++ & 3];\n\tsize_t o = 0;\n\n\tif (!s)\n\t\treturn \"NULL\";\n\tb[o++] = '\"';\n\tfor (; *s && o < 150; s++) {\n\t\tif (*s == '\\n') {\n\t\t\tb[o++] = '\\\\';\n\t\t\tb[o++] = 'n';\n\t\t} else if (*s == '\\t') {\n\t\t\tb[o++] = '\\\\';\n\t\t\tb[o++] = 't';\n\t\t} else if (*s == '\"' || *s == '\\\\') {\n\t\t\tb[o++] = '\\\\';\n\t\t\tb[o++] = *s;\n\t\t} else {\n\t\t\tb[o++] = *s;\n\t\t}\n\t}\n\tif (*s) {\n\t\tmemcpy(b + o, \"...\", 3);\n\t\to += 3;\n\t}\n\tb[o++] = '\"';\n\tb[o] = '\\0';\n\treturn b;\n}\n\nstatic ini_doc *parse_ok(const char *what, const char *text)\n{\n\tini_doc *doc = NULL;\n\tint line = -1;\n\tint rc = ini_parse_string(text, &doc, &line);\n\n\tif (rc != INI_OK)\n\t\tfail(\"%s: parse returned %d (%s) at line %d, expected INI_OK\", what, rc, ini_strerror(rc), line);\n\tif (!doc)\n\t\tfail(\"%s: parse returned INI_OK but *out is NULL\", what);\n\tif (line != 0)\n\t\tfail(\"%s: *err_line is %d after a successful parse, expected 0\", what, line);\n\treturn doc;\n}\n\nstatic void expect_str(const ini_doc *doc, const char *sec, const char *key, const char *want)\n{\n\tconst char *got = ini_get(doc, sec, key);\n\n\tif (!want && got)\n\t\tfail(\"[%s] %s: expected no entry, got %s\", sec, key, show(got));\n\tif (want && (!got || strcmp(got, want) != 0))\n\t\tfail(\"[%s] %s: expected %s, got %s\", sec, key, show(want), show(got));\n}\n\nstatic void expect_count(const ini_doc *doc, size_t want)\n{\n\tif (ini_count(doc) != want)\n\t\tfail(\"ini_count: expected %zu, got %zu\", want, ini_count(doc));\n}\n\nstatic void expect_syntax(const char *what, const char *text, int want_line)\n{\n\tini_doc *doc = (ini_doc *)&msg_fd;\n\tint line = -1;\n\tint rc = ini_parse_string(text, &doc, &line);\n\n\tif (rc != INI_ERR_SYNTAX)\n\t\tfail(\"%s: expected INI_ERR_SYNTAX, got %d (%s)\", what, rc, ini_strerror(rc));\n\tif (line != want_line)\n\t\tfail(\"%s: expected error on line %d, got line %d\", what, want_line, line);\n\tif (doc != NULL)\n\t\tfail(\"%s: *out must be NULL after an error\", what);\n}\n\nstatic void expect_int(const ini_doc *doc, const char *key, int want_rc, long want)\n{\n\tlong v = 12345;\n\tint rc = ini_get_int(doc, \"n\", key, &v);\n\n\tif (rc != want_rc)\n\t\tfail(\"ini_get_int(%s = %s): expected %s, got %d (%s)\", key, show(ini_get(doc, \"n\", key)),\n\t\t     ini_strerror(want_rc), rc, ini_strerror(rc));\n\tif (want_rc == INI_OK && v != want)\n\t\tfail(\"ini_get_int(%s = %s): expected %ld, got %ld\", key, show(ini_get(doc, \"n\", key)), want, v);\n\tif (want_rc != INI_OK && v != 12345)\n\t\tfail(\"ini_get_int(%s = %s): *out was written on error (%ld)\", key, show(ini_get(doc, \"n\", key)), v);\n}\n\n/* ---------------------------------------------------------------- checks */\n\nstatic void t_basic_pairs(void)\n{\n\tini_doc *doc = parse_ok(\"basic\", \"name = demo\\n[server]\\nhost = example.org\\nport=8080\\n\"\n\t\t\t\t\t \"[client]\\nport = 9\\n\");\n\texpect_count(doc, 4);\n\texpect_str(doc, \"\", \"name\", \"demo\");\n\texpect_str(doc, \"server\", \"host\", \"example.org\");\n\texpect_str(doc, \"SERVER\", \"Host\", \"example.org\");\n\texpect_str(doc, \"server\", \"port\", \"8080\");\n\texpect_str(doc, \"client\", \"port\", \"9\");\n\texpect_str(doc, \"server\", \"name\", NULL);\n\texpect_str(doc, \"nosuch\", \"port\", NULL);\n\tini_free(doc);\n}\n\nstatic void t_comments(void)\n{\n\tini_doc *doc = parse_ok(\"comments\", \"; top comment\\n\"\n\t\t\t\t\t    \"  # indented comment\\n\"\n\t\t\t\t\t    \"\\n\"\n\t\t\t\t\t    \"[ main ]   ; section comment\\n\"\n\t\t\t\t\t    \"a = 1 ; note\\n\"\n\t\t\t\t\t    \"b = x;y\\n\"\n\t\t\t\t\t    \"url = http://host/a#top\\n\"\n\t\t\t\t\t    \"c = v\\t# note\\n\"\n\t\t\t\t\t    \"d = ; only a comment\\n\"\n\t\t\t\t\t    \"e=#hash\\n\");\n\texpect_count(doc, 6);\n\texpect_str(doc, \"main\", \"a\", \"1\");\n\texpect_str(doc, \"main\", \"b\", \"x;y\");\n\texpect_str(doc, \"main\", \"url\", \"http://host/a#top\");\n\texpect_str(doc, \"main\", \"c\", \"v\");\n\texpect_str(doc, \"main\", \"d\", \"\");\n\texpect_str(doc, \"main\", \"e\", \"\");\n\tini_free(doc);\n}\n\nstatic void t_line_endings(void)\n{\n\tini_doc *doc = parse_ok(\"crlf\", \"[w]\\r\\na = 1\\r\\nb=two words \\r\\n\\r\\nc = last\");\n\texpect_count(doc, 3);\n\texpect_str(doc, \"w\", \"a\", \"1\");\n\texpect_str(doc, \"w\", \"b\", \"two words\");\n\texpect_str(doc, \"w\", \"c\", \"last\");\n\tini_free(doc);\n\tdoc = parse_ok(\"empty document\", \"\");\n\texpect_count(doc, 0);\n\tini_free(doc);\n}\n\nstatic void t_quoted_values(void)\n{\n\tini_doc *doc = parse_ok(\"quoted\", \"[q]\\n\"\n\t\t\t\t\t  \"pad = \\\"  padded  \\\"\\n\"\n\t\t\t\t\t  \"semi = \\\"semi ; colon # hash\\\" ; trailing comment\\n\"\n\t\t\t\t\t  \"esc = \\\"tab\\\\there\\\\nnext\\\"\\n\"\n\t\t\t\t\t  \"path = \\\"C:\\\\\\\\dir\\\\\\\\sub\\\"\\n\"\n\t\t\t\t\t  \"empty = \\\"\\\"\\n\");\n\texpect_str(doc, \"q\", \"pad\", \"  padded  \");\n\texpect_str(doc, \"q\", \"semi\", \"semi ; colon # hash\");\n\texpect_str(doc, \"q\", \"esc\", \"tab\\there\\nnext\");\n\texpect_str(doc, \"q\", \"path\", \"C:\\\\dir\\\\sub\");\n\texpect_str(doc, \"q\", \"empty\", \"\");\n\tini_free(doc);\n}\n\nstatic void t_escaped_quotes(void)\n{\n\tini_doc *doc = parse_ok(\"escaped quotes\", \"[q]\\n\"\n\t\t\t\t\t\t  \"say = \\\"she said \\\\\\\"hi\\\\\\\" twice\\\"\\n\"\n\t\t\t\t\t\t  \"only = \\\"\\\\\\\"\\\"\\n\"\n\t\t\t\t\t\t  \"dir = \\\"a\\\\\\\\\\\" ; ends with a backslash\\n\"\n\t\t\t\t\t\t  \"mix = \\\"x\\\\\\\\\\\\\\\"y\\\"\\n\");\n\texpect_str(doc, \"q\", \"say\", \"she said \\\"hi\\\" twice\");\n\texpect_str(doc, \"q\", \"only\", \"\\\"\");\n\texpect_str(doc, \"q\", \"dir\", \"a\\\\\");\n\texpect_str(doc, \"q\", \"mix\", \"x\\\\\\\"y\");\n\tini_free(doc);\n}\n\nstatic void t_syntax_errors(void)\n{\n\texpect_syntax(\"line without '='\", \"[ok]\\nx = 1\\nnot a pair\\n\", 3);\n\texpect_syntax(\"unterminated section\", \"a=1\\n[unterminated\\n\", 2);\n\texpect_syntax(\"empty section name\", \"[  ]\\n\", 1);\n\texpect_syntax(\"text after section\", \"[a] b\\n\", 1);\n\texpect_syntax(\"empty key\", \"[a]\\n = v\\n\", 2);\n\texpect_syntax(\"unterminated quote\", \"[a]\\nk = \\\"open\\nz = 1\\n\", 2);\n\texpect_syntax(\"text after closing quote\", \"k = \\\"v\\\" junk\\n\", 1);\n\texpect_syntax(\"unknown escape\", \"\\n\\nk = \\\"bad \\\\q\\\"\\n\", 3);\n\texpect_syntax(\"crlf line count\", \"a=1\\r\\nb=2\\r\\noops\\r\\n\", 3);\n}\n\nstatic void t_duplicates(void)\n{\n\tini_doc *doc = parse_ok(\"duplicates\", \"[s]\\na = 1\\nA = 2\\n[t]\\na = 3\\n[S]\\na = 4\\nb = 5\\n\");\n\texpect_count(doc, 3);\n\texpect_str(doc, \"s\", \"a\", \"4\");\n\texpect_str(doc, \"t\", \"a\", \"3\");\n\texpect_str(doc, \"s\", \"b\", \"5\");\n\tini_free(doc);\n}\n\nstatic void t_continuation(void)\n{\n\tini_doc *doc = parse_ok(\"continuation\", \"[d]\\n\"\n\t\t\t\t\t\t\"desc = first\\n\"\n\t\t\t\t\t\t\"  second part\\n\"\n\t\t\t\t\t\t\"\\tthird ; comment\\n\"\n\t\t\t\t\t\t\"next = 1\\n\"\n\t\t\t\t\t\t\"other = x\\n\"\n\t\t\t\t\t\t\"\\n\"\n\t\t\t\t\t\t\"  key2 = not a continuation\\n\"\n\t\t\t\t\t\t\"q = \\\"quoted\\\"\\n\"\n\t\t\t\t\t\t\"    more\\n\");\n\texpect_str(doc, \"d\", \"desc\", \"first second part third\");\n\texpect_str(doc, \"d\", \"next\", \"1\");\n\texpect_str(doc, \"d\", \"other\", \"x\");\n\texpect_str(doc, \"d\", \"key2\", \"not a continuation\");\n\texpect_str(doc, \"d\", \"q\", \"quoted more\");\n\texpect_count(doc, 5);\n\tini_free(doc);\n}\n\nstatic void t_long_values(void)\n{\n\tchar *text = malloc(64 * 1024), *want = malloc(16 * 1024);\n\tsize_t o = 0, w = 0;\n\tchar key[16], val[16];\n\n\to += (size_t)sprintf(text + o, \"[big]\\n\");\n\tfor (int i = 0; i < 40; i++)\n\t\to += (size_t)sprintf(text + o, \"k%d = v%d\\n\", i, i);\n\to += (size_t)sprintf(text + o, \"long = w0\\n\");\n\tw += (size_t)sprintf(want + w, \"w0\");\n\tfor (int i = 1; i < 300; i++) {\n\t\to += (size_t)sprintf(text + o, \"   w%d\\n\", i);\n\t\tw += (size_t)sprintf(want + w, \" w%d\", i);\n\t}\n\to += (size_t)sprintf(text + o, \"after = done\\n\");\n\tini_doc *doc = parse_ok(\"long document\", text);\n\texpect_count(doc, 42);\n\tfor (int i = 0; i < 40; i += 7) {\n\t\tsprintf(key, \"k%d\", i);\n\t\tsprintf(val, \"v%d\", i);\n\t\texpect_str(doc, \"big\", key, val);\n\t}\n\tconst char *got = ini_get(doc, \"big\", \"long\");\n\tif (!got || strcmp(got, want) != 0)\n\t\tfail(\"value continued over 300 lines: expected %zu chars, got %s\", strlen(want),\n\t\t     got ? \"a different value\" : \"NULL\");\n\texpect_str(doc, \"big\", \"after\", \"done\");\n\tini_free(doc);\n\tfree(text);\n\tfree(want);\n}\n\nstatic void t_unescape(void)\n{\n\tchar *buf;\n\tint r;\n\n\tbuf = malloc(4);\n\tr = ini_unescape(\"abc\", 3, buf, 4);\n\tif (r != 3 || strcmp(buf, \"abc\") != 0)\n\t\tfail(\"ini_unescape(\\\"abc\\\", outsz 4): expected 3 \\\"abc\\\", got %d\", r);\n\tfree(buf);\n\n\tbuf = malloc(3);\n\tr = ini_unescape(\"abc\", 3, buf, 3);\n\tif (r != INI_ERR_RANGE)\n\t\tfail(\"ini_unescape(\\\"abc\\\", outsz 3): expected INI_ERR_RANGE (no room for the NUL), got %d\", r);\n\tfree(buf);\n\n\tbuf = malloc(4);\n\tr = ini_unescape(\"a\\\\nb\", 4, buf, 4);\n\tif (r != 3 || strcmp(buf, \"a\\nb\") != 0)\n\t\tfail(\"ini_unescape(\\\"a\\\\\\\\nb\\\", outsz 4): expected 3, got %d\", r);\n\tfree(buf);\n\n\tbuf = malloc(1);\n\tr = ini_unescape(\"\", 0, buf, 1);\n\tif (r != 0 || buf[0] != '\\0')\n\t\tfail(\"ini_unescape(\\\"\\\", outsz 1): expected 0 and an empty string, got %d\", r);\n\tr = ini_unescape(\"x\", 1, buf, 1);\n\tif (r != INI_ERR_RANGE)\n\t\tfail(\"ini_unescape(\\\"x\\\", outsz 1): expected INI_ERR_RANGE, got %d\", r);\n\tfree(buf);\n\n\tbuf = malloc(8);\n\tr = ini_unescape(\"ab\\\\\", 3, buf, 8);\n\tif (r != INI_ERR_SYNTAX)\n\t\tfail(\"ini_unescape with a trailing backslash: expected INI_ERR_SYNTAX, got %d\", r);\n\tr = ini_unescape(\"\\\\x\", 2, buf, 8);\n\tif (r != INI_ERR_SYNTAX)\n\t\tfail(\"ini_unescape(\\\"\\\\\\\\x\\\"): expected INI_ERR_SYNTAX, got %d\", r);\n\tfree(buf);\n}\n\nstatic void t_int_values(void)\n{\n\tini_doc *doc = parse_ok(\"ints\", \"[n]\\na = 10\\nb = -42\\nc = +7\\nd = 0x1F\\ne = -0X10\\n\"\n\t\t\t\t\t\"max = 9223372036854775807\\nmin = -9223372036854775808\\nz = 0\\n\");\n\texpect_int(doc, \"a\", INI_OK, 10);\n\texpect_int(doc, \"b\", INI_OK, -42);\n\texpect_int(doc, \"c\", INI_OK, 7);\n\texpect_int(doc, \"d\", INI_OK, 31);\n\texpect_int(doc, \"e\", INI_OK, -16);\n\texpect_int(doc, \"max\", INI_OK, LONG_MAX);\n\texpect_int(doc, \"min\", INI_OK, LONG_MIN);\n\texpect_int(doc, \"z\", INI_OK, 0);\n\tini_free(doc);\n}\n\nstatic void t_int_errors(void)\n{\n\tini_doc *doc = parse_ok(\"int errors\", \"[n]\\njunk = 12abc\\nempty =\\nword = abc\\nsp = \\\" 5\\\"\\nhex = 0x\\n\"\n\t\t\t\t\t      \"sign = +-5\\nbig = 9223372036854775808\\nsmall = -9223372036854775809\\n\"\n\t\t\t\t\t      \"bighex = 0x8000000000000000\\nhuge = 123456789012345678901234567890\\n\");\n\texpect_int(doc, \"missing\", INI_ERR_NOTFOUND, 0);\n\texpect_int(doc, \"junk\", INI_ERR_TYPE, 0);\n\texpect_int(doc, \"empty\", INI_ERR_TYPE, 0);\n\texpect_int(doc, \"word\", INI_ERR_TYPE, 0);\n\texpect_int(doc, \"sp\", INI_ERR_TYPE, 0);\n\texpect_int(doc, \"hex\", INI_ERR_TYPE, 0);\n\texpect_int(doc, \"sign\", INI_ERR_TYPE, 0);\n\texpect_int(doc, \"big\", INI_ERR_RANGE, 0);\n\texpect_int(doc, \"small\", INI_ERR_RANGE, 0);\n\texpect_int(doc, \"bighex\", INI_ERR_RANGE, 0);\n\texpect_int(doc, \"huge\", INI_ERR_RANGE, 0);\n\tini_free(doc);\n}\n\nstatic void t_parse_file(void)\n{\n\tconst char *dir = getenv(\"TMPDIR\");\n\tchar path[512];\n\tFILE *f;\n\tini_doc *doc = NULL;\n\tint line = -1, rc;\n\n\tsnprintf(path, sizeof path, \"%s/libini-check.ini\", dir ? dir : \".\");\n\tf = fopen(path, \"wb\");\n\tif (!f)\n\t\tfail(\"could not create a temporary file\");\n\tfputs(\"[file]\\nk = from file\\r\\n  and more\\n\", f);\n\tfclose(f);\n\trc = ini_parse_file(path, &doc, &line);\n\tif (rc != INI_OK)\n\t\tfail(\"ini_parse_file: expected INI_OK, got %d (%s)\", rc, ini_strerror(rc));\n\texpect_str(doc, \"file\", \"k\", \"from file and more\");\n\tini_free(doc);\n\tremove(path);\n\tdoc = (ini_doc *)&msg_fd;\n\trc = ini_parse_file(path, &doc, &line);\n\tif (rc != INI_ERR_IO || doc != NULL)\n\t\tfail(\"ini_parse_file on a missing file: expected INI_ERR_IO and NULL, got %d\", rc);\n}\n\n/* ---------------------------------------------------------------- harness */\n\nstatic size_t drain(int fd, char *buf, size_t cap)\n{\n\tsize_t len = 0;\n\tchar tmp[4096];\n\tssize_t n;\n\n\twhile ((n = read(fd, tmp, sizeof tmp)) > 0) {\n\t\tsize_t take = (size_t)n;\n\t\tif (take > cap - 1 - len)\n\t\t\ttake = cap - 1 - len;\n\t\tmemcpy(buf + len, tmp, take);\n\t\tlen += take;\n\t}\n\tbuf[len] = '\\0';\n\treturn len;\n}\n\nstatic const char *base_name(const char *p, const char *end)\n{\n\tconst char *b = p;\n\tfor (const char *q = p; q < end; q++)\n\t\tif (*q == '/')\n\t\t\tb = q + 1;\n\treturn b;\n}\n\n/* First stack frame after `from` that lies in the library (not the sanitizer runtime, not the\n * checks): \"func (file.c:line)\". */\nstatic int lib_frame(const char *from, char *out, size_t cap)\n{\n\tconst char *l = from;\n\n\twhile ((l = strstr(l, \"\\n    #\")) != NULL) {\n\t\tconst char *eol = strchr(l + 1, '\\n');\n\t\tconst char *in = strstr(l, \" in \");\n\t\tl++;\n\t\tif (!eol)\n\t\t\teol = l + strlen(l);\n\t\tif (!in || in > eol)\n\t\t\tcontinue;\n\t\tif (memmem(l, (size_t)(eol - l), \"__checker__\", 11) || memmem(l, (size_t)(eol - l), \"libasan\", 7) ||\n\t\t    memmem(l, (size_t)(eol - l), \"libubsan\", 8))\n\t\t\tcontinue;\n\t\tconst char *fn = in + 4, *sp = memchr(fn, ' ', (size_t)(eol - fn));\n\t\tif (!sp || !memmem(sp, (size_t)(eol - sp), \".c:\", 3))\n\t\t\tcontinue;\n\t\tconst char *b = base_name(sp + 1, eol);\n\t\tsnprintf(out, cap, \"%.*s (%.*s)\", (int)(sp - fn), fn, (int)(eol - b), b);\n\t\treturn 1;\n\t}\n\treturn 0;\n}\n\n/* Turn a sanitizer report into a one-line reason that names the library location. */\nstatic void describe_crash(const char *err, char *out, size_t cap)\n{\n\tconst char *e = strstr(err, \"ERROR: AddressSanitizer: \");\n\tconst char *u = strstr(err, \"runtime error: \");\n\n\tif (e) {\n\t\tconst char *kind = e + strlen(\"ERROR: AddressSanitizer: \");\n\t\tsize_t klen = strcspn(kind, \" \\n\");\n\t\tchar at[200] = \"\", freed[200] = \"\";\n\t\tconst char *fr = strstr(e, \"freed by thread\");\n\t\tint has_at = lib_frame(e, at, sizeof at);\n\t\tint has_fr = fr && lib_frame(fr, freed, sizeof freed);\n\t\tsnprintf(out, cap, \"memory error (AddressSanitizer): %.*s%s%s%s%s\", (int)klen, kind,\n\t\t\t has_at ? \" in \" : \"\", has_at ? at : \"\", has_fr ? \"; the memory was freed in \" : \"\",\n\t\t\t has_fr ? freed : \"\");\n\t} else if (u) {\n\t\tconst char *ls = u;\n\t\twhile (ls > err && ls[-1] != '\\n')\n\t\t\tls--;\n\t\tconst char *eol = strchr(u, '\\n');\n\t\tif (!eol)\n\t\t\teol = u + strlen(u);\n\t\tconst char *msg = u + strlen(\"runtime error: \");\n\t\tconst char *b = base_name(ls, u);\n\t\tint in_checker = memmem(ls, (size_t)(u - ls), \"__checker__\", 11) != NULL;\n\t\tsnprintf(out, cap, \"undefined behaviour: %.*s%s%.*s\", (int)(eol - msg), msg,\n\t\t\t in_checker ? \"\" : \" at \", in_checker ? 0 : (int)(u - 2 - b), b);\n\t} else {\n\t\tout[0] = '\\0';\n\t}\n}\n\nstatic void run(const char *name, void (*fn)(void))\n{\n\tint mp[2], ep[2], st;\n\tchar msg[600], err[65536], why[500];\n\tpid_t pid;\n\n\tfflush(stdout);\n\tif (pipe(mp) || pipe(ep)) {\n\t\tprintf(\"TEST %s FAIL could not create a pipe\\n\", name);\n\t\treturn;\n\t}\n\tpid = fork();\n\tif (pid == 0) {\n\t\tclose(mp[0]);\n\t\tclose(ep[0]);\n\t\tdup2(ep[1], 2);\n\t\tmsg_fd = mp[1];\n\t\talarm(20);\n\t\tfn();\n\t\t_exit(0);\n\t}\n\tclose(mp[1]);\n\tclose(ep[1]);\n\tdrain(ep[0], err, sizeof err);\n\tdrain(mp[0], msg, sizeof msg);\n\tclose(mp[0]);\n\tclose(ep[0]);\n\twaitpid(pid, &st, 0);\n\tif (WIFEXITED(st) && WEXITSTATUS(st) == 0) {\n\t\tprintf(\"TEST %s PASS\\n\", name);\n\t} else if (msg[0]) {\n\t\tprintf(\"TEST %s FAIL %s\\n\", name, msg);\n\t} else {\n\t\tdescribe_crash(err, why, sizeof why);\n\t\tif (why[0])\n\t\t\tprintf(\"TEST %s FAIL %s\\n\", name, why);\n\t\telse if (WIFSIGNALED(st) && WTERMSIG(st) == SIGALRM)\n\t\t\tprintf(\"TEST %s FAIL timed out after 20s\\n\", name);\n\t\telse if (WIFSIGNALED(st))\n\t\t\tprintf(\"TEST %s FAIL crashed with signal %d\\n\", name, WTERMSIG(st));\n\t\telse\n\t\t\tprintf(\"TEST %s FAIL exited with status %d\\n\", name, WEXITSTATUS(st));\n\t}\n\tfflush(stdout);\n}\n\nint main(void)\n{\n\trun(\"basic_pairs\", t_basic_pairs);\n\trun(\"comments\", t_comments);\n\trun(\"line_endings\", t_line_endings);\n\trun(\"quoted_values\", t_quoted_values);\n\trun(\"escaped_quotes\", t_escaped_quotes);\n\trun(\"syntax_errors\", t_syntax_errors);\n\trun(\"duplicate_keys\", t_duplicates);\n\trun(\"continuation_lines\", t_continuation);\n\trun(\"long_values\", t_long_values);\n\trun(\"unescape_bounds\", t_unescape);\n\trun(\"int_values\", t_int_values);\n\trun(\"int_errors\", t_int_errors);\n\trun(\"parse_file\", t_parse_file);\n\treturn 0;\n}\n"
 },
 "hidden_dir": "__checker__",
 "steps": [
  {
   "cmd": [
    "make"
   ],
   "label": "make",
   "timeout_s": 120
  },
  {
   "cmd": [
    "sh",
    "-c",
    "out=$(gcc -std=c11 -g -O1 -fno-omit-frame-pointer -fsanitize=address,undefined -fno-sanitize-recover=undefined -Iinclude -Isrc src/*.c __checker__/check.c -o chk 2>&1) || { printf '%s\\n' \"$out\" | grep -E '^(src|include)/[^ ]*:[0-9]+:[0-9]+: (fatal )?error'; echo 'the hidden checks do not compile against this library: keep the API of include/ini.h (README.md)'; exit 1; }"
   ],
   "label": "compile checks",
   "timeout_s": 120
  },
  {
   "cmd": [
    "./chk"
   ],
   "label": "tests",
   "tests": true,
   "timeout_s": 120
  }
 ],
 "tests": [
  "basic_pairs",
  "comments",
  "line_endings",
  "quoted_values",
  "escaped_quotes",
  "syntax_errors",
  "duplicate_keys",
  "continuation_lines",
  "long_values",
  "unescape_bounds",
  "int_values",
  "int_errors",
  "parse_file"
 ]
};
// The checker every training task ships as `run_tests`: the agent may run it, and cannot read it.
//
// make_task.py prepends `const SPEC = {...};` (the task's hidden test files, its build and test
// steps, the test names it must see) and build.mjs (manic_miner's) turns the result into a Node
// single executable whose source is masked.
//
// It copies the working directory to a private temporary directory, writes the hidden tests into
// it, runs the steps there with the language's own tools, and prints ONE JSON line:
//
//   {"ok":true|false,"stage":"build"|"test"|"done","passed":N,"total":N,"failures":[...],"output":"..."}
//
// Test programs report one line per test on stdout:  TEST <name> PASS   or   TEST <name> FAIL <message>
// "ok" needs every step to exit 0 and every name in SPEC.tests to report PASS: a test that never
// reports (the program died, or exited early) is a failure. Output lines that point into the hidden
// test directory are dropped, so a build error never prints the tests' source.
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const cp = require("node:child_process");

const HIDDEN = SPEC.hidden_dir || "__checker__";
const MAX_OUT = 6000;

function copyTree(src, dst) {
	fs.mkdirSync(dst, { recursive: true });
	for (const e of fs.readdirSync(src, { withFileTypes: true })) {
		if (e.name === ".git" || e.name === "run_tests" || e.name === HIDDEN) continue;
		const s = path.join(src, e.name);
		const d = path.join(dst, e.name);
		if (e.isDirectory()) copyTree(s, d);
		else if (e.isFile()) fs.copyFileSync(s, d);
	}
}

// A failure message is the test's own words, but an error inside it (a stack, a module path) can name a
// hidden file: any token that contains the hidden directory's name is replaced.
function hide(text) {
	return String(text).replace(new RegExp(`\\S*${HIDDEN}\\S*`, "g"), "<checker>");
}

// A compiler names the file on one line and quotes its source on the next ones ("  12 | code", a "^~~~"
// marker line): after a line that names a hidden file, those quoted lines are dropped as well.
const QUOTED = /^\s*(\d+\s*)?\|/;
const MARKER = /^\s*\|?\s*[\^~\-+]+[\s\^~\-+]*$/;

function scrub(text) {
	const lines = [];
	let quoting = false;
	for (const l of String(text || "").split("\n")) {
		if (l.includes(HIDDEN)) {
			quoting = true;
			continue;
		}
		if (quoting && (QUOTED.test(l) || MARKER.test(l) || /^\s*$/.test(l))) continue;
		quoting = false;
		if (!/^TEST /.test(l)) lines.push(l);
	}
	const out = lines.join("\n").trim();
	return out.length > MAX_OUT ? `${out.slice(0, MAX_OUT)}\n[... output cut]` : out;
}

function finish(res, work) {
	try {
		fs.rmSync(work, { recursive: true, force: true });
	} catch {}
	process.stdout.write(`${JSON.stringify(res)}\n`);
	process.exit(0);
}

const work = fs.mkdtempSync(path.join(os.tmpdir(), "chk-"));
const res = { ok: false, stage: "build", passed: 0, total: SPEC.tests.length, failures: [], output: "" };
try {
	copyTree(process.cwd(), work);
	for (const [rel, body] of Object.entries(SPEC.files)) {
		const p = path.join(work, rel);
		fs.mkdirSync(path.dirname(p), { recursive: true });
		fs.writeFileSync(p, body);
	}
} catch (e) {
	res.output = `could not prepare the check: ${e.message}`;
	finish(res, work);
}

const env = {
	PATH: process.env.PATH || "/usr/local/bin:/usr/bin:/bin",
	HOME: work,
	TMPDIR: work,
	LANG: "C.UTF-8",
	...(SPEC.env || {}),
};
for (const [k, v] of Object.entries(env)) env[k] = String(v).replaceAll("$WORK", work);

const seen = new Map();
let output = "";
for (const step of SPEC.steps) {
	const cmd = step.cmd.map((a) => String(a).replaceAll("$WORK", work));
	const r = cp.spawnSync(cmd[0], cmd.slice(1), {
		cwd: work,
		env,
		encoding: "utf8",
		timeout: (step.timeout_s || 120) * 1000,
		maxBuffer: 64 * 1024 * 1024,
	});
	const text = `${r.stdout || ""}\n${r.stderr || ""}`;
	if (step.tests) {
		res.stage = "test";
		for (const m of String(r.stdout || "").matchAll(/^TEST (\S+) (PASS|FAIL)(?: (.*))?$/gm)) {
			if (!seen.has(m[1]) || m[2] === "FAIL") seen.set(m[1], m[2] === "PASS" ? null : hide(m[3] || "failed").slice(0, 300));
		}
	}
	output += `${scrub(text)}\n`;
	if (r.error && r.error.code === "ETIMEDOUT") {
		res.output = scrub(`${output}\nstep timed out after ${step.timeout_s || 120}s: ${step.label || step.cmd[0]}`);
		break;
	}
	if (r.error) {
		res.output = scrub(`${output}\ncould not run ${cmd[0]}: ${r.error.message}`);
		break;
	}
	if (r.status !== 0 && !step.tests) {
		res.output = scrub(`${output}\n${step.label || step.cmd[0]} failed (exit ${r.status})`);
		break;
	}
	if (step.tests && r.status !== 0 && seen.size === 0) {
		res.output = scrub(`${output}\nthe tests did not run (exit ${r.status})`);
		break;
	}
}
for (const name of SPEC.tests) {
	if (!seen.has(name)) res.failures.push({ test: name, message: "did not run" });
	else if (seen.get(name) !== null) res.failures.push({ test: name, message: seen.get(name) });
	else res.passed += 1;
}
if (res.stage === "test" && !res.output) {
	res.stage = "done";
	res.output = scrub(output);
}
res.ok = res.stage === "done" && res.passed === res.total;
finish(res, work);
