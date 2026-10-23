use std::fmt;

/// Everything that can go wrong while tokenizing, parsing or evaluating an expression.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum Error {
    /// A character that cannot start a token. `column` is 1-based and counts characters.
    UnexpectedChar { ch: char, column: usize },
    /// A token that is not allowed where it appears.
    UnexpectedToken { column: usize },
    /// The input ended in the middle of an expression.
    UnexpectedEnd,
    UnknownVariable(String),
    UnknownFunction(String),
    /// A built-in function called with the wrong number of arguments.
    Arity { name: String, got: usize },
    DivisionByZero,
    NegativeExponent,
    /// A literal or an intermediate result does not fit in an `i64`.
    Overflow,
}

impl fmt::Display for Error {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Error::UnexpectedChar { ch, column } => write!(f, "unexpected character {ch:?} at column {column}"),
            Error::UnexpectedToken { column } => write!(f, "unexpected token at column {column}"),
            Error::UnexpectedEnd => write!(f, "unexpected end of input"),
            Error::UnknownVariable(name) => write!(f, "unknown variable `{name}`"),
            Error::UnknownFunction(name) => write!(f, "unknown function `{name}`"),
            Error::Arity { name, got } => write!(f, "`{name}` does not take {got} argument(s)"),
            Error::DivisionByZero => write!(f, "division by zero"),
            Error::NegativeExponent => write!(f, "negative exponent"),
            Error::Overflow => write!(f, "integer overflow"),
        }
    }
}

impl std::error::Error for Error {}
