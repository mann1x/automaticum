use crate::error::Error;

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum Token {
    Number(i64),
    Ident(String),
    Plus,
    Minus,
    Star,
    Slash,
    Percent,
    Caret,
    LParen,
    RParen,
    Comma,
}

/// A token and the 1-based column (in characters) where it starts.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Spanned {
    pub token: Token,
    pub column: usize,
}

pub fn tokenize(src: &str) -> Result<Vec<Spanned>, Error> {
    let mut out = Vec::new();
    let mut it = src.char_indices().map(|(i, c)| (i + 1, i, c)).peekable();

    while let Some(&(column, i, c)) = it.peek() {
        if c.is_whitespace() {
            it.next();
            continue;
        }
        if c.is_ascii_digit() {
            let mut digits = String::new();
            while let Some(&(_, _, d)) = it.peek() {
                if d.is_ascii_digit() {
                    digits.push(d);
                } else if d != '_' {
                    break;
                }
                it.next();
            }
            let n = digits.parse::<i64>().map_err(|_| Error::Overflow)?;
            out.push(Spanned { token: Token::Number(n), column });
            continue;
        }
        if c.is_alphabetic() || c == '_' {
            let mut end = i;
            while let Some(&(_, j, d)) = it.peek() {
                if !(d.is_alphanumeric() || d == '_') {
                    break;
                }
                end = j + d.len_utf8();
                it.next();
            }
            out.push(Spanned { token: Token::Ident(src[i..end].to_string()), column });
            continue;
        }
        let token = match c {
            '+' => Token::Plus,
            '-' => Token::Minus,
            '*' => Token::Star,
            '/' => Token::Slash,
            '%' => Token::Percent,
            '^' => Token::Caret,
            '(' => Token::LParen,
            ')' => Token::RParen,
            ',' => Token::Comma,
            _ => return Err(Error::UnexpectedChar { ch: c, column }),
        };
        it.next();
        out.push(Spanned { token, column });
    }
    Ok(out)
}
