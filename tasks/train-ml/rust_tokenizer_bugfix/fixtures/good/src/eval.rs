use std::collections::HashMap;

use crate::error::Error;
use crate::parser::{Expr, Op};

/// Variable bindings for an evaluation.
pub type Env = HashMap<String, i64>;

pub fn eval(expr: &Expr, env: &Env) -> Result<i64, Error> {
    match expr {
        Expr::Num(n) => Ok(*n),
        Expr::Var(name) => env.get(name).copied().ok_or_else(|| Error::UnknownVariable(name.clone())),
        Expr::Neg(inner) => eval(inner, env)?.checked_neg().ok_or(Error::Overflow),
        Expr::Bin(op, lhs, rhs) => {
            let a = eval(lhs, env)?;
            let b = eval(rhs, env)?;
            binary(*op, a, b)
        }
        Expr::Call(name, args) => call(name, args, env),
    }
}

fn binary(op: Op, a: i64, b: i64) -> Result<i64, Error> {
    match op {
        Op::Add => a.checked_add(b).ok_or(Error::Overflow),
        Op::Sub => a.checked_sub(b).ok_or(Error::Overflow),
        Op::Mul => a.checked_mul(b).ok_or(Error::Overflow),
        Op::Div => {
            if b == 0 {
                return Err(Error::DivisionByZero);
            }
            a.checked_div(b).ok_or(Error::Overflow)
        }
        Op::Rem => {
            if b == 0 {
                return Err(Error::DivisionByZero);
            }
            a.checked_rem(b).ok_or(Error::Overflow)
        }
        Op::Pow => {
            if b < 0 {
                return Err(Error::NegativeExponent);
            }
            let exp = u32::try_from(b).map_err(|_| Error::Overflow)?;
            a.checked_pow(exp).ok_or(Error::Overflow)
        }
    }
}

fn call(name: &str, args: &[Expr], env: &Env) -> Result<i64, Error> {
    let arity_ok = match name {
        "abs" => args.len() == 1,
        "min" | "max" => !args.is_empty(),
        _ => return Err(Error::UnknownFunction(name.to_string())),
    };
    if !arity_ok {
        return Err(Error::Arity { name: name.to_string(), got: args.len() });
    }
    let values = args.iter().map(|a| eval(a, env)).collect::<Result<Vec<_>, _>>()?;
    match name {
        "abs" => values[0].checked_abs().ok_or(Error::Overflow),
        "min" => Ok(*values.iter().min().unwrap()),
        _ => Ok(*values.iter().max().unwrap()),
    }
}
