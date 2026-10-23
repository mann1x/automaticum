use std::process::ExitCode;

use exprcalc::{evaluate, Env};

fn main() -> ExitCode {
    let mut args = std::env::args().skip(1);
    let Some(expr) = args.next() else {
        eprintln!("usage: exprcalc EXPR [NAME=VALUE ...]");
        return ExitCode::from(2);
    };
    let mut env = Env::new();
    for binding in args {
        let Some((name, value)) = binding.split_once('=') else {
            eprintln!("exprcalc: expected NAME=VALUE, got {binding}");
            return ExitCode::from(2);
        };
        match value.trim().parse::<i64>() {
            Ok(v) => {
                env.insert(name.trim().to_string(), v);
            }
            Err(_) => {
                eprintln!("exprcalc: {name}: not an integer: {value}");
                return ExitCode::from(2);
            }
        }
    }
    match evaluate(&expr, &env) {
        Ok(v) => {
            println!("{v}");
            ExitCode::SUCCESS
        }
        Err(e) => {
            eprintln!("exprcalc: {e}");
            ExitCode::FAILURE
        }
    }
}
