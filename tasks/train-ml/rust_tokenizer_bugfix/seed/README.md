# exprcalc

Evaluates integer expressions such as `2 * (width + 3) ^ 2 - max(a, b)`. A library crate with a tiny
CLI; no dependencies.

```sh
cargo test
cargo run -- '2 ^ 3 ^ 2' 
cargo run -- 'größe * 2' größe=21
```

Library API: `exprcalc::evaluate(src, &env) -> Result<i64, exprcalc::Error>`, where `env` is an
`exprcalc::Env` (`HashMap<String, i64>`). The stages are public too: `lexer::tokenize`,
`parser::parse`, `eval::eval`.

## Tokens

* **Whitespace** is any Unicode whitespace character and separates tokens.
* **Numbers** are a decimal digit followed by any digits and underscores (`1_000_000`); the underscores
  are ignored. A literal that does not fit in an `i64` is `Error::Overflow`.
* **Identifiers** start with a Unicode letter (`char::is_alphabetic`) or `_` and continue with Unicode
  letters, digits (`char::is_alphanumeric`) or `_`: `x`, `_tmp1`, `größe`, `π`, `сумма`.
* **Operators**: `+ - * / % ^ ( ) ,`.
* Anything else is `Error::UnexpectedChar { ch, column }`.

Every token carries its **column: the 1-based position counted in characters** (not bytes) from the
start of the input. Error columns are token columns.

## Grammar and precedence (lowest first)

| level | operators | associativity |
|---|---|---|
| sum | binary `+`, `-` | left |
| product | `*`, `/`, `%` | left |
| unary | prefix `-` | right (`--3` is 3) |
| power | `^` | **right** (`2 ^ 3 ^ 2` is `2 ^ 9` = 512) |
| primary | number, variable, `name(arg, ...)`, `( sum )` | |

`^` binds tighter than unary minus: `-2 ^ 2` is `-(2 ^ 2)` = -4, while `(-2) ^ 2` is 4. The exponent
of `^` may itself carry a unary minus: `2 ^ -1` parses (and fails with `NegativeExponent`).

Parse errors: a token where it is not allowed is `UnexpectedToken { column }`; running out of input in
the middle of an expression (including an empty input) is `UnexpectedEnd`.

## Evaluation

All arithmetic is on `i64` and is **checked**: any result that does not fit — including `-i64::MIN`,
`i64::MIN / -1`, `abs(i64::MIN)` and a too-large power — is `Error::Overflow`; nothing panics or wraps.

* `/` truncates toward zero, `%` has the sign of the dividend (`-7 / 2` = -3, `-7 % 2` = -1).
  Division or remainder by zero is `Error::DivisionByZero`.
* `a ^ b` with `b < 0` is `Error::NegativeExponent`; `0 ^ 0` is 1.
* Variables come from `env`; a missing one is `Error::UnknownVariable(name)`.
* Built-in functions: `abs(x)` (exactly one argument), `min(...)` and `max(...)` (one or more). A
  wrong argument count is `Error::Arity { name, got }`; any other name is
  `Error::UnknownFunction(name)`. Arguments are evaluated left to right.
