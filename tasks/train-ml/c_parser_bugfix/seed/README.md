# libini

A small INI-file parser in C11 (no dependencies). It builds a static library `libini.a`, a
command-line tool `inidump`, and a test program.

```sh
make            # libini.a + inidump
make test       # builds and runs tests/test_ini
./inidump config.ini              # print every entry as section.key=value
./inidump config.ini server port  # print one value
```

## API (`include/ini.h`)

| function | |
|---|---|
| `int ini_parse_string(const char *text, ini_doc **out, int *err_line)` | parse a whole document |
| `int ini_parse_file(const char *path, ini_doc **out, int *err_line)` | read a file and parse it |
| `void ini_free(ini_doc *doc)` | free a document (NULL is allowed) |
| `const char *ini_get(const ini_doc *doc, const char *section, const char *key)` | value or NULL |
| `int ini_get_int(const ini_doc *doc, const char *section, const char *key, long *out)` | integer value |
| `size_t ini_count(const ini_doc *doc)` | number of distinct (section, key) entries |
| `int ini_unescape(const char *in, size_t len, char *out, size_t outsz)` | decode escapes |
| `const char *ini_strerror(int code)` | text for an error code |

Error codes are the negative `INI_ERR_*` constants; `INI_OK` is 0.

## Syntax

* The text is read line by line. Lines end in `\n`; a `\r` right before it is ignored. The last line
  does not need a newline.
* Blank lines are ignored. A line whose first non-blank character is `;` or `#` is a comment.
* `[name]` starts a section. The name is trimmed and must not be empty. After the `]` only blanks or a
  comment may follow. Keys that appear before any section header belong to the section `""`.
* `key = value` defines an entry. The key is the text before the first `=`, trimmed, and must not be
  empty. A non-blank line that is neither a comment, a section nor contains `=` is an error.
* **Unquoted values** are trimmed. A `;` or `#` that is the first character of the value or follows a
  space or tab starts a comment, which is dropped (with the blanks before it). So
  `url = http://host/a#top` keeps the `#top`, `n = 5 ; five` gives `5`.
* **Quoted values** start with `"` and end at the matching closing `"` that is not escaped. Inside the
  quotes everything is literal (including `;`, `#` and leading/trailing spaces) except the escapes
  `\"`, `\\`, `\n` and `\t`; any other escape is an error. After the closing quote only blanks or a
  comment may follow. A missing closing quote is an error.
* **Continuation lines:** a line that starts with a space or tab and comes directly after a key line
  (or after another continuation line) continues that value: its text, handled as an unquoted value,
  is appended to the value with a single space between. A blank or comment line ends the value.
* A key that appears twice in the same section keeps the **last** value.
* Section and key names are compared case-insensitively (ASCII) by the lookup functions.

## Errors and results

* `ini_parse_string` / `ini_parse_file` return `INI_OK` and set `*out`, or return an error code and
  set `*out` to NULL. For a syntax error the result is `INI_ERR_SYNTAX` and `*err_line` is the 1-based
  number of the first bad line; on success `*err_line` is 0. `err_line` may be NULL.
* `ini_get_int` accepts an optional `+`/`-` sign followed by decimal digits, or by `0x`/`0X` and hex
  digits; the whole value must be the number. It returns `INI_ERR_NOTFOUND` if the entry does not
  exist, `INI_ERR_TYPE` if the value is not an integer (an empty value is not), and `INI_ERR_RANGE`
  if it does not fit in a `long`. `*out` is written only on success.
* `ini_unescape` decodes the escapes above in `in[0..len)` and writes the result plus a terminating
  NUL into `out`, which has room for `outsz` bytes. It returns the decoded length (without the NUL),
  `INI_ERR_RANGE` if the result and its NUL do not fit (nothing is ever written past `outsz` bytes),
  or `INI_ERR_SYNTAX` for an unknown or trailing backslash escape.
