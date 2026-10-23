use std::io::Read;
use std::process::ExitCode;

use csvcore::stats::column_sum;
use csvcore::{write_record, Table};

const USAGE: &str = "usage: csvtool count FILE\n       csvtool columns FILE\n       csvtool sum FILE COLUMN\n       csvtool select FILE COLUMN[,COLUMN...]";

enum Command<'a> {
    Count,
    Columns,
    Sum(&'a str),
    Select(&'a str),
}

fn read_input(path: &str) -> Result<String, String> {
    if path == "-" {
        let mut text = String::new();
        std::io::stdin()
            .read_to_string(&mut text)
            .map_err(|e| format!("cannot read standard input: {e}"))?;
        return Ok(text);
    }
    std::fs::read_to_string(path).map_err(|e| format!("cannot read {path}: {e}"))
}

fn column(table: &Table, name: &str) -> Result<usize, String> {
    table.column(name).ok_or_else(|| format!("no column named {name:?}"))
}

fn run(cmd: Command<'_>, path: &str) -> Result<String, String> {
    let text = read_input(path)?;
    let table = Table::parse(&text).map_err(|e| e.to_string())?;
    let mut out = String::new();
    match cmd {
        Command::Count => out.push_str(&format!("{}\n", table.rows.len())),
        Command::Columns => {
            for h in &table.header {
                out.push_str(h);
                out.push('\n');
            }
        }
        Command::Sum(name) => {
            let c = column(&table, name)?;
            let sum = column_sum(&table, c).map_err(|e| e.to_string())?;
            out.push_str(&format!("{sum}\n"));
        }
        Command::Select(list) => {
            let cols = list.split(',').map(|n| column(&table, n)).collect::<Result<Vec<_>, _>>()?;
            let pick = |row: &[String]| cols.iter().map(|&c| row[c].clone()).collect::<Vec<_>>();
            out.push_str(&write_record(&pick(&table.header)));
            out.push('\n');
            for row in &table.rows {
                out.push_str(&write_record(&pick(row)));
                out.push('\n');
            }
        }
    }
    Ok(out)
}

fn main() -> ExitCode {
    let args: Vec<String> = std::env::args().skip(1).collect();
    let parsed = match args.iter().map(String::as_str).collect::<Vec<_>>().as_slice() {
        ["count", file] => Some((Command::Count, file.to_string())),
        ["columns", file] => Some((Command::Columns, file.to_string())),
        ["sum", file, col] => Some((Command::Sum(*col), file.to_string())),
        ["select", file, cols] => Some((Command::Select(*cols), file.to_string())),
        _ => None,
    };
    let Some((cmd, file)) = parsed else {
        eprintln!("{USAGE}");
        return ExitCode::from(2);
    };
    match run(cmd, &file) {
        Ok(out) => {
            print!("{out}");
            ExitCode::SUCCESS
        }
        Err(e) => {
            eprintln!("csvtool: {e}");
            ExitCode::from(1)
        }
    }
}
