use exprcalc::{evaluate, Env, Error};

fn calc(src: &str) -> Result<i64, Error> {
    evaluate(src, &Env::new())
}

#[test]
fn arithmetic() {
    assert_eq!(calc("1 + 2 * 3"), Ok(7));
    assert_eq!(calc("(1 + 2) * 3"), Ok(9));
    assert_eq!(calc("1_000 - 1"), Ok(999));
    assert_eq!(calc("2 ^ 10"), Ok(1024));
}

#[test]
fn variables() {
    let mut env = Env::new();
    env.insert("width".into(), 4);
    assert_eq!(evaluate("width * 2", &env), Ok(8));
    assert_eq!(evaluate("height", &env), Err(Error::UnknownVariable("height".into())));
}

#[test]
fn errors() {
    assert_eq!(calc("1 / 0"), Err(Error::DivisionByZero));
    assert_eq!(calc("1 +"), Err(Error::UnexpectedEnd));
    assert_eq!(calc("1 $ 2"), Err(Error::UnexpectedChar { ch: '$', column: 3 }));
}
