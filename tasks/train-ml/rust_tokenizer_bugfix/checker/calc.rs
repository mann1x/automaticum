// Hidden checks for exprcalc. One #[test] runs every named check under catch_unwind and prints the
// TEST protocol line for each (run with --nocapture).
use std::panic::{self, AssertUnwindSafe};
use std::sync::Mutex;

use exprcalc::lexer::{tokenize, Spanned, Token};
use exprcalc::{evaluate, Env, Error};

static PANIC_MSG: Mutex<String> = Mutex::new(String::new());

type Check = Result<(), String>;

fn env(pairs: &[(&str, i64)]) -> Env {
    pairs.iter().map(|(k, v)| (k.to_string(), *v)).collect()
}

fn expect_with(e: &Env, src: &str, want: Result<i64, Error>) -> Check {
    let got = evaluate(src, e);
    if got == want {
        Ok(())
    } else {
        Err(format!("evaluate({src:?}): expected {want:?}, got {got:?}"))
    }
}

fn expect(src: &str, want: Result<i64, Error>) -> Check {
    expect_with(&Env::new(), src, want)
}

fn literals_and_whitespace() -> Check {
    expect("42", Ok(42))?;
    expect(" \t1_000_000 +\n2 ", Ok(1_000_002))?;
    expect("1\u{3000}+\u{a0}2", Ok(3))?;
    expect("9223372036854775807", Ok(i64::MAX))?;
    expect("0", Ok(0))
}

fn precedence() -> Check {
    expect("2 + 3 * 4", Ok(14))?;
    expect("(2 + 3) * 4", Ok(20))?;
    expect("10 - 4 - 3", Ok(3))?;
    expect("100 / 10 / 5", Ok(2))?;
    expect("7 % 4 * 3", Ok(9))?;
    expect("2 * 3 ^ 2", Ok(18))?;
    expect("1 + 2 ^ 2 * 3", Ok(13))
}

fn power_associativity() -> Check {
    expect("2 ^ 3 ^ 2", Ok(512))?;
    expect("(2 ^ 3) ^ 2", Ok(64))?;
    expect("2 ^ 2 ^ 2 ^ 2", Ok(65536))?;
    expect("2 ^ 10", Ok(1024))?;
    expect("0 ^ 0", Ok(1))?;
    expect("(-1) ^ 3 ^ 2", Ok(-1))
}

fn unary_minus() -> Check {
    expect("-2 ^ 2", Ok(-4))?;
    expect("(-2) ^ 2", Ok(4))?;
    expect("-3 ^ 3", Ok(-27))?;
    expect("--3", Ok(3))?;
    expect("2 * -3", Ok(-6))?;
    expect("2 ^ -1", Err(Error::NegativeExponent))?;
    expect("2 ^ --2", Ok(4))?;
    expect("-(4 - 6)", Ok(2))?;
    expect("1 - -1", Ok(2))
}

fn unicode_identifiers() -> Check {
    let e = env(&[("größe", 21), ("π", 3), ("сумма", 10), ("_tmp1", 7), ("été2", 1)]);
    expect_with(&e, "größe * 2", Ok(42))?;
    expect_with(&e, "π^2", Ok(9))?;
    expect_with(&e, "сумма - _tmp1 + été2", Ok(4))?;
    expect_with(&e, "max(größe, π)", Ok(21))?;
    expect_with(&e, "größ", Err(Error::UnknownVariable("größ".into())))
}

fn token_columns() -> Check {
    let want = |src: &str, toks: Vec<(Token, usize)>| -> Check {
        let got = tokenize(src).map_err(|e| format!("tokenize({src:?}): unexpected error {e:?}"))?;
        let want: Vec<Spanned> = toks.into_iter().map(|(token, column)| Spanned { token, column }).collect();
        if got == want {
            Ok(())
        } else {
            Err(format!("tokenize({src:?}): expected {want:?}, got {got:?}"))
        }
    };
    want("x ^ 2", vec![(Token::Ident("x".into()), 1), (Token::Caret, 3), (Token::Number(2), 5)])?;
    want("é+1", vec![(Token::Ident("é".into()), 1), (Token::Plus, 2), (Token::Number(1), 3)])?;
    want(
        "größe*(1_0)",
        vec![
            (Token::Ident("größe".into()), 1),
            (Token::Star, 6),
            (Token::LParen, 7),
            (Token::Number(10), 8),
            (Token::RParen, 11),
        ],
    )
}

fn error_columns() -> Check {
    expect("1 $ 2", Err(Error::UnexpectedChar { ch: '$', column: 3 }))?;
    let e = env(&[("größe", 1), ("ü", 2)]);
    expect_with(&e, "größe + #", Err(Error::UnexpectedChar { ch: '#', column: 9 }))?;
    expect_with(&e, "ü ü", Err(Error::UnexpectedToken { column: 3 }))?;
    expect("\u{3000}\u{3000}1 + )", Err(Error::UnexpectedToken { column: 7 }))?;
    expect("1 + €", Err(Error::UnexpectedChar { ch: '€', column: 5 }))
}

fn overflow() -> Check {
    expect("9223372036854775807 + 1", Err(Error::Overflow))?;
    expect("-9223372036854775807 - 2", Err(Error::Overflow))?;
    expect("9223372036854775807 * 2", Err(Error::Overflow))?;
    expect("3037000500 * 3037000500", Err(Error::Overflow))?;
    expect("9223372036854775808", Err(Error::Overflow))?;
    expect("-(-9223372036854775807 - 1)", Err(Error::Overflow))?;
    expect("(-9223372036854775807 - 1) / -1", Err(Error::Overflow))?;
    expect("(-9223372036854775807 - 1) % -1", Err(Error::Overflow))?;
    expect("2 ^ 63", Err(Error::Overflow))?;
    expect("2 ^ 4294967296", Err(Error::Overflow))?;
    expect("abs(-9223372036854775807 - 1)", Err(Error::Overflow))?;
    expect("-9223372036854775807 - 1", Ok(i64::MIN))?;
    expect("-2 ^ 63", Err(Error::Overflow))?;
    expect("(-2) ^ 63", Ok(i64::MIN))
}

fn division() -> Check {
    expect("7 / 2", Ok(3))?;
    expect("-7 / 2", Ok(-3))?;
    expect("-7 % 2", Ok(-1))?;
    expect("7 % -2", Ok(1))?;
    expect("1 / 0", Err(Error::DivisionByZero))?;
    expect("5 % 0", Err(Error::DivisionByZero))?;
    expect("5 % (3 - 3)", Err(Error::DivisionByZero))
}

fn variables_and_functions() -> Check {
    let e = env(&[("x", 6), ("y", -4)]);
    expect_with(&e, "x * y", Ok(-24))?;
    expect_with(&e, "x * z", Err(Error::UnknownVariable("z".into())))?;
    expect_with(&e, "max(1, x, 3)", Ok(6))?;
    expect_with(&e, "min(x, y) + abs(y)", Ok(0))?;
    expect_with(&e, "max(y)", Ok(-4))?;
    expect_with(&e, "abs(1, 2)", Err(Error::Arity { name: "abs".into(), got: 2 }))?;
    expect_with(&e, "max()", Err(Error::Arity { name: "max".into(), got: 0 }))?;
    expect_with(&e, "foo(1)", Err(Error::UnknownFunction("foo".into())))?;
    expect_with(&e, "abs(-x) ^ 2", Ok(36))
}

fn syntax_errors() -> Check {
    expect("", Err(Error::UnexpectedEnd))?;
    expect("1 +", Err(Error::UnexpectedEnd))?;
    expect("(1 + 2", Err(Error::UnexpectedEnd))?;
    expect("1 2", Err(Error::UnexpectedToken { column: 3 }))?;
    expect("max(1,)", Err(Error::UnexpectedToken { column: 7 }))?;
    expect("()", Err(Error::UnexpectedToken { column: 2 }))?;
    expect("2 ^ ^ 3", Err(Error::UnexpectedToken { column: 5 }))
}

fn run(name: &str, f: fn() -> Check) {
    PANIC_MSG.lock().unwrap().clear();
    let line = match panic::catch_unwind(AssertUnwindSafe(f)) {
        Ok(Ok(())) => format!("TEST {name} PASS"),
        Ok(Err(msg)) => format!("TEST {name} FAIL {}", msg.replace('\n', " ")),
        Err(_) => format!("TEST {name} FAIL panicked: {}", PANIC_MSG.lock().unwrap().replace('\n', " ")),
    };
    println!("{line}");
}

#[test]
fn hidden_checks() {
    panic::set_hook(Box::new(|info| {
        let msg = if let Some(s) = info.payload().downcast_ref::<&str>() {
            s.to_string()
        } else if let Some(s) = info.payload().downcast_ref::<String>() {
            s.clone()
        } else {
            "unknown panic".to_string()
        };
        let at = match info.location() {
            Some(l) if !l.file().contains("__checker__") => format!(" at {}:{}", l.file(), l.line()),
            _ => String::new(),
        };
        *PANIC_MSG.lock().unwrap() = format!("{msg}{at}");
    }));
    run("literals_and_whitespace", literals_and_whitespace);
    run("precedence", precedence);
    run("power_associativity", power_associativity);
    run("unary_minus", unary_minus);
    run("unicode_identifiers", unicode_identifiers);
    run("token_columns", token_columns);
    run("error_columns", error_columns);
    run("overflow", overflow);
    run("division", division);
    run("variables_and_functions", variables_and_functions);
    run("syntax_errors", syntax_errors);
}
