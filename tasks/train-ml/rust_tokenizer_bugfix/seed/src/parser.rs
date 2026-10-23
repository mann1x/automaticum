use crate::error::Error;
use crate::lexer::{Spanned, Token};

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Op {
    Add,
    Sub,
    Mul,
    Div,
    Rem,
    Pow,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum Expr {
    Num(i64),
    Var(String),
    Neg(Box<Expr>),
    Bin(Op, Box<Expr>, Box<Expr>),
    Call(String, Vec<Expr>),
}

/// Parse a whole token stream into one expression.
pub fn parse(tokens: &[Spanned]) -> Result<Expr, Error> {
    let mut p = Parser { tokens, pos: 0 };
    let e = p.sum()?;
    match p.peek() {
        None => Ok(e),
        Some(t) => Err(Error::UnexpectedToken { column: t.column }),
    }
}

struct Parser<'a> {
    tokens: &'a [Spanned],
    pos: usize,
}

impl<'a> Parser<'a> {
    fn peek(&self) -> Option<&'a Spanned> {
        self.tokens.get(self.pos)
    }

    fn eat(&mut self, want: &Token) -> bool {
        if self.peek().map(|t| &t.token) == Some(want) {
            self.pos += 1;
            true
        } else {
            false
        }
    }

    fn next(&mut self) -> Result<&'a Spanned, Error> {
        let t = self.peek().ok_or(Error::UnexpectedEnd)?;
        self.pos += 1;
        Ok(t)
    }

    // sum := product (('+' | '-') product)*
    fn sum(&mut self) -> Result<Expr, Error> {
        let mut lhs = self.product()?;
        loop {
            let op = if self.eat(&Token::Plus) {
                Op::Add
            } else if self.eat(&Token::Minus) {
                Op::Sub
            } else {
                return Ok(lhs);
            };
            let rhs = self.product()?;
            lhs = Expr::Bin(op, Box::new(lhs), Box::new(rhs));
        }
    }

    // product := power (('*' | '/' | '%') power)*
    fn product(&mut self) -> Result<Expr, Error> {
        let mut lhs = self.power()?;
        loop {
            let op = if self.eat(&Token::Star) {
                Op::Mul
            } else if self.eat(&Token::Slash) {
                Op::Div
            } else if self.eat(&Token::Percent) {
                Op::Rem
            } else {
                return Ok(lhs);
            };
            let rhs = self.power()?;
            lhs = Expr::Bin(op, Box::new(lhs), Box::new(rhs));
        }
    }

    fn power(&mut self) -> Result<Expr, Error> {
        let mut lhs = self.unary()?;
        while self.eat(&Token::Caret) {
            let rhs = self.unary()?;
            lhs = Expr::Bin(Op::Pow, Box::new(lhs), Box::new(rhs));
        }
        Ok(lhs)
    }

    fn unary(&mut self) -> Result<Expr, Error> {
        if self.eat(&Token::Minus) {
            return Ok(Expr::Neg(Box::new(self.unary()?)));
        }
        self.primary()
    }

    fn primary(&mut self) -> Result<Expr, Error> {
        let t = self.next()?;
        match &t.token {
            Token::Number(n) => Ok(Expr::Num(*n)),
            Token::LParen => {
                let e = self.sum()?;
                self.close_paren()?;
                Ok(e)
            }
            Token::Ident(name) => {
                if !self.eat(&Token::LParen) {
                    return Ok(Expr::Var(name.clone()));
                }
                let mut args = Vec::new();
                if !self.eat(&Token::RParen) {
                    loop {
                        args.push(self.sum()?);
                        if self.eat(&Token::Comma) {
                            continue;
                        }
                        self.close_paren()?;
                        break;
                    }
                }
                Ok(Expr::Call(name.clone(), args))
            }
            _ => Err(Error::UnexpectedToken { column: t.column }),
        }
    }

    fn close_paren(&mut self) -> Result<(), Error> {
        let t = self.next()?;
        if t.token == Token::RParen {
            Ok(())
        } else {
            Err(Error::UnexpectedToken { column: t.column })
        }
    }
}
