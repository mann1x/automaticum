//! `exprcalc`: evaluate integer expressions such as `2 * (width + 3) ^ 2 - max(a, b)`.
//!
//! See README.md for the grammar and the error rules.

pub mod error;
pub mod eval;
pub mod lexer;
pub mod parser;

pub use error::Error;
pub use eval::Env;

/// Tokenize, parse and evaluate `src` with the variables in `env`.
pub fn evaluate(src: &str, env: &Env) -> Result<i64, Error> {
    let tokens = lexer::tokenize(src)?;
    let expr = parser::parse(&tokens)?;
    eval::eval(&expr, env)
}
