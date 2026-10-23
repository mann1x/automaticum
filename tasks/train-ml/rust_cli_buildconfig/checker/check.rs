// Hidden checks for the csvtool workspace: the csvcore library (linked from the workspace build) and
// the csvtool binary (run as a process). Prints one TEST line per check.
use std::io::Write;
use std::panic::{self, AssertUnwindSafe};
use std::path::{Path, PathBuf};
use std::process::{Command, Stdio};
use std::sync::Mutex;

use csvcore::stats::{column_sum, StatsError};
use csvcore::{parse_line, write_record, CsvError, LineError, Table};

static PANIC_MSG: Mutex<String> = Mutex::new(String::new());

type Check = Result<(), String>;

fn ensure(cond: bool, msg: impl FnOnce() -> String) -> Check {
    if cond {
        Ok(())
    } else {
        Err(msg())
    }
}

fn work_dir() -> PathBuf {
    let dir = PathBuf::from(std::env::var("TMPDIR").unwrap_or_else(|_| ".".into())).join("csvchk");
    std::fs::create_dir_all(&dir).expect("create the check directory");
    dir
}

fn write_file(name: &str, text: &str) -> PathBuf {
    let p = work_dir().join(name);
    std::fs::write(&p, text).expect("write a check file");
    p
}

fn binary() -> PathBuf {
    let target = std::env::var("CARGO_TARGET_DIR").unwrap_or_else(|_| "target".into());
    Path::new(&target).join("debug").join("csvtool")
}

struct Out {
    code: Option<i32>,
    stdout: String,
    stderr: String,
}

fn csvtool(args: &[&str], stdin: Option<&str>) -> Result<Out, String> {
    let bin = binary();
    if !bin.exists() {
        return Err("`cargo build` did not produce a binary named csvtool (target/debug/csvtool)".into());
    }
    let mut child = Command::new(&bin)
        .args(args)
        .stdin(if stdin.is_some() { Stdio::piped() } else { Stdio::null() })
        .stdout(Stdio::piped())
        .stderr(Stdio::piped())
        .spawn()
        .map_err(|e| format!("could not run csvtool: {e}"))?;
    if let Some(text) = stdin {
        child.stdin.take().unwrap().write_all(text.as_bytes()).map_err(|e| e.to_string())?;
    }
    let o = child.wait_with_output().map_err(|e| e.to_string())?;
    Ok(Out {
        code: o.status.code(),
        stdout: String::from_utf8_lossy(&o.stdout).into_owned(),
        stderr: String::from_utf8_lossy(&o.stderr).into_owned(),
    })
}

fn show_args(args: &[&str]) -> String {
    args.iter()
        .map(|a| if a.contains("csvchk") { Path::new(a).file_name().unwrap().to_string_lossy().into_owned() } else { a.to_string() })
        .collect::<Vec<_>>()
        .join(" ")
}

fn expect_ok(args: &[&str], stdin: Option<&str>, want: &str) -> Check {
    let o = csvtool(args, stdin)?;
    let what = format!("csvtool {}", show_args(args));
    ensure(o.code == Some(0), || format!("{what}: expected exit 0, got {:?} (stderr {:?})", o.code, o.stderr.trim_end()))?;
    ensure(o.stdout == want, || format!("{what}: expected output {want:?}, got {:?}", o.stdout))
}

fn expect_err(args: &[&str], code: i32, stderr_prefix: &str) -> Check {
    let o = csvtool(args, None)?;
    let what = format!("csvtool {}", show_args(args));
    ensure(o.code == Some(code), || format!("{what}: expected exit {code}, got {:?} (stderr {:?})", o.code, o.stderr.trim_end()))?;
    ensure(o.stderr.starts_with(stderr_prefix), || {
        format!("{what}: expected stderr starting with {stderr_prefix:?}, got {:?}", o.stderr.trim_end())
    })?;
    ensure(o.stdout.is_empty(), || format!("{what}: expected nothing on stdout, got {:?}", o.stdout))
}

fn fields(v: &[&str]) -> Vec<String> {
    v.iter().map(|s| s.to_string()).collect()
}

fn expect_line(line: &str, want: &[&str]) -> Check {
    let got = parse_line(line);
    ensure(got == Ok(fields(want)), || format!("parse_line({line:?}): expected Ok({want:?}), got {got:?}"))
}

// ---------------------------------------------------------------- library

fn lib_parse_line() -> Check {
    expect_line("a,b,c", &["a", "b", "c"])?;
    expect_line("a,,b", &["a", "", "b"])?;
    expect_line("a,", &["a", ""])?;
    expect_line(" x , y ", &[" x ", " y "])?;
    expect_line("\"Bolt, M6\",3", &["Bolt, M6", "3"])?;
    expect_line("\"\",x", &["", "x"])?;
    let e = parse_line("\"abc,d");
    ensure(e == Err(LineError::UnterminatedQuote), || format!("parse_line(\"\\\"abc,d\"): expected UnterminatedQuote, got {e:?}"))?;
    let e = parse_line("\"a\"b,c");
    ensure(e == Err(LineError::TextAfterQuote), || format!("parse_line(\"\\\"a\\\"b,c\"): expected TextAfterQuote, got {e:?}"))
}

fn lib_quote_escape() -> Check {
    expect_line("\"say \"\"hi\"\"\",x", &["say \"hi\"", "x"])?;
    expect_line("\"\"\"\"", &["\""])?;
    expect_line("a,\"\"\"quoted\"\"\"", &["a", "\"quoted\""])?;
    expect_line("\"x\"\"\",\"\"", &["x\"", ""])?;
    let e = parse_line("\"abc\"\"");
    ensure(e == Err(LineError::UnterminatedQuote), || format!("parse_line(\"\\\"abc\\\"\\\"\"): expected UnterminatedQuote, got {e:?}"))
}

fn lib_table() -> Check {
    let t = Table::parse("id,name\r\n1,ann\r\n\r\n  \n2,\"bo, b\"\n").map_err(|e| format!("Table::parse: unexpected error {e:?}"))?;
    ensure(t.header == fields(&["id", "name"]), || format!("header: got {:?}", t.header))?;
    ensure(t.rows == vec![fields(&["1", "ann"]), fields(&["2", "bo, b"])], || format!("rows: got {:?}", t.rows))?;
    ensure(t.lines == vec![2, 5], || format!("line numbers: expected [2, 5], got {:?}", t.lines))?;
    ensure(t.column("name") == Some(1) && t.column("Name").is_none(), || "Table::column lookup is wrong".into())?;
    let e = Table::parse("a,b\n1,2\n\n1,2,3\n");
    ensure(e == Err(CsvError::FieldCount { line: 4, expected: 2, found: 3 }), || format!("ragged row: got {e:?}"))?;
    let e = Table::parse("a,b\n\"x,1\n");
    ensure(e == Err(CsvError::Line { line: 2, error: LineError::UnterminatedQuote }), || format!("bad quote on line 2: got {e:?}"))?;
    let e = Table::parse("\n \n");
    ensure(e == Err(CsvError::Empty), || format!("blank input: expected Empty, got {e:?}"))?;
    let t = Table::parse("only,header\n").map_err(|e| format!("header only: {e:?}"))?;
    ensure(t.rows.is_empty(), || "header only: expected no rows".into())
}

fn lib_write_record() -> Check {
    let cases: Vec<Vec<&str>> = vec![
        vec!["a", "b"],
        vec!["x,y", "z"],
        vec!["say \"hi\"", ""],
        vec!["\"", ",", " sp "],
        vec![""],
    ];
    let w = write_record(&["a", "b,c", "d\"e", ""]);
    ensure(w == "a,\"b,c\",\"d\"\"e\",", || format!("write_record: got {w:?}"))?;
    for c in cases {
        let line = write_record(&c);
        let back = parse_line(&line);
        ensure(back == Ok(fields(&c)), || format!("parse_line(write_record({c:?})) = {back:?} (line {line:?})"))?;
    }
    Ok(())
}

fn lib_column_sum() -> Check {
    let t = Table::parse("item,qty,price\na,3,1.5\nb,,2.25\nc, 4 ,0.25\nd,  ,-1\n").map_err(|e| format!("{e:?}"))?;
    let q = column_sum(&t, 1);
    ensure(q == Ok(7.0), || format!("column_sum(qty) with empty cells: expected Ok(7.0), got {q:?}"))?;
    let p = column_sum(&t, 2);
    ensure(p == Ok(3.0), || format!("column_sum(price): expected Ok(3.0), got {p:?}"))?;
    let t = Table::parse("n\n1\n\nx 2\n").map_err(|e| format!("{e:?}"))?;
    let e = column_sum(&t, 0);
    ensure(e == Err(StatsError::NotANumber { line: 4, value: "x 2".into() }), || format!("non-numeric cell: got {e:?}"))?;
    let t = Table::parse("n\n").map_err(|e| format!("{e:?}"))?;
    ensure(column_sum(&t, 0) == Ok(0.0), || "sum of no rows: expected 0".into())
}

// ---------------------------------------------------------------- CLI

const STOCK: &str = "sku,name,qty,price\nA-100,\"Bolt, M6\",120,0.15\nA-101,Washer,,0.02\n\nB-200,\"Hex \"\"Allen\"\" key\",4,3.5\n";

fn cli_binary() -> Check {
    let o = csvtool(&[], None)?;
    ensure(o.code == Some(2), || format!("csvtool with no arguments: expected exit 2, got {:?}", o.code))?;
    ensure(o.stderr.starts_with("usage:"), || format!("csvtool with no arguments: expected usage on stderr, got {:?}", o.stderr))
}

fn cli_count() -> Check {
    let f = write_file("stock.csv", STOCK);
    expect_ok(&["count", f.to_str().unwrap()], None, "3\n")?;
    let f = write_file("crlf.csv", "a,b\r\n1,2\r\n3,4\r\n");
    expect_ok(&["count", f.to_str().unwrap()], None, "2\n")?;
    let f = write_file("header.csv", "a,b\n");
    expect_ok(&["count", f.to_str().unwrap()], None, "0\n")?;
    expect_ok(&["count", "-"], Some("x\n1\n2\n3\n4\n"), "4\n")
}

fn cli_columns() -> Check {
    let f = write_file("stock.csv", STOCK);
    expect_ok(&["columns", f.to_str().unwrap()], None, "sku\nname\nqty\nprice\n")?;
    expect_ok(&["columns", "-"], Some("\"a,b\",\"say \"\"x\"\"\"\n1,2\n"), "a,b\nsay \"x\"\n")
}

fn cli_sum() -> Check {
    let f = write_file("stock.csv", STOCK);
    let p = f.to_str().unwrap();
    expect_ok(&["sum", p, "qty"], None, "124\n")?;
    expect_ok(&["sum", p, "price"], None, "3.67\n")?;
    expect_ok(&["sum", "-", "v"], Some("v\n0.1\n0.2\n"), "0.30000000000000004\n")?;
    expect_ok(&["sum", "-", "v"], Some("v\n 2.5 \n\n-4\n  \n"), "-1.5\n")
}

fn cli_sum_errors() -> Check {
    let f = write_file("bad.csv", "item,qty\na,3\nb,lots\n");
    let p = f.to_str().unwrap();
    expect_err(&["sum", p, "qty"], 1, "csvtool: line 3: not a number: \"lots\"")?;
    expect_err(&["sum", p, "nope"], 1, "csvtool: no column named \"nope\"")?;
    let f = write_file("stock.csv", STOCK);
    expect_err(&["sum", f.to_str().unwrap(), "name"], 1, "csvtool: line 2: not a number: \"Bolt, M6\"")
}

fn cli_select() -> Check {
    let f = write_file("stock.csv", STOCK);
    let p = f.to_str().unwrap();
    expect_ok(
        &["select", p, "name,sku"],
        None,
        "name,sku\n\"Bolt, M6\",A-100\nWasher,A-101\n\"Hex \"\"Allen\"\" key\",B-200\n",
    )?;
    expect_ok(&["select", p, "qty"], None, "qty\n120\n\n4\n")?;
    expect_ok(&["select", p, "sku,sku"], None, "sku,sku\nA-100,A-100\nA-101,A-101\nB-200,B-200\n")?;
    expect_err(&["select", p, "sku,colour"], 1, "csvtool: no column named \"colour\"")
}

fn cli_errors() -> Check {
    let missing = work_dir().join("missing.csv");
    expect_err(&["count", missing.to_str().unwrap()], 1, "csvtool: cannot read ")?;
    let f = write_file("ragged.csv", "a,b\n1,2\n1,2,3\n");
    expect_err(&["count", f.to_str().unwrap()], 1, "csvtool: line 3: expected 2 fields, found 3")?;
    let f = write_file("quote.csv", "a,b\n\"x,1\n");
    expect_err(&["columns", f.to_str().unwrap()], 1, "csvtool: line 2: unterminated quoted field")?;
    let f = write_file("after.csv", "a,b\n\"x\"y,1\n");
    expect_err(&["count", f.to_str().unwrap()], 1, "csvtool: line 2: unexpected text after a closing quote")?;
    let f = write_file("empty.csv", "");
    expect_err(&["count", f.to_str().unwrap()], 1, "csvtool: empty input (no header line)")?;
    let f = write_file("stock.csv", STOCK);
    expect_err(&["avg", f.to_str().unwrap(), "qty"], 2, "usage:")?;
    expect_err(&["count"], 2, "usage:")?;
    expect_err(&["count", f.to_str().unwrap(), "extra"], 2, "usage:")
}

// ---------------------------------------------------------------- harness

fn run(name: &str, f: fn() -> Check) {
    PANIC_MSG.lock().unwrap().clear();
    let line = match panic::catch_unwind(AssertUnwindSafe(f)) {
        Ok(Ok(())) => format!("TEST {name} PASS"),
        Ok(Err(msg)) => format!("TEST {name} FAIL {}", msg.replace('\n', " ")),
        Err(_) => format!("TEST {name} FAIL panicked: {}", PANIC_MSG.lock().unwrap().replace('\n', " ")),
    };
    println!("{line}");
}

fn main() {
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
    run("lib_parse_line", lib_parse_line);
    run("lib_quote_escape", lib_quote_escape);
    run("lib_table", lib_table);
    run("lib_write_record", lib_write_record);
    run("lib_column_sum", lib_column_sum);
    run("cli_binary", cli_binary);
    run("cli_count", cli_count);
    run("cli_columns", cli_columns);
    run("cli_sum", cli_sum);
    run("cli_sum_errors", cli_sum_errors);
    run("cli_select", cli_select);
    run("cli_errors", cli_errors);
}
