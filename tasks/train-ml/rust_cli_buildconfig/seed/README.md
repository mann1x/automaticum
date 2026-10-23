# csvtool

A small CSV inspection tool, as a Cargo workspace (Rust 2021, no external crates):

| path | crate | what |
|---|---|---|
| `crates/csvcore` | library `csvcore` | CSV reading/writing; column statistics behind the `stats` feature |
| `crates/csvtool` | package `csvtool-cli`, binary **`csvtool`** | the command-line front end |

```sh
cargo build                          # builds target/debug/csvtool and the csvcore library
cargo test
target/debug/csvtool sum examples/stock.csv qty
```

## CSV format

* A record is one line; `\n` and `\r\n` line endings are accepted. Lines that are empty or contain only
  blanks are skipped. The first record is the header. A quoted field cannot contain a line break.
* Fields are separated by `,`. Unquoted fields are taken exactly as written (no trimming).
* A field that starts with `"` is quoted: it ends at the next `"` that is not doubled; inside it `""`
  stands for one `"` and `,` is literal. After the closing quote only `,` or the end of the line may
  follow (`LineError::TextAfterQuote`); a missing closing quote is `LineError::UnterminatedQuote`.
* Every data record must have as many fields as the header (`CsvError::FieldCount`).
* Line numbers in errors are 1-based line numbers in the input, counting skipped blank lines.

## Library (`csvcore`)

* `parse_line(&str) -> Result<Vec<String>, LineError>` — split one line.
* `write_record(&[S]) -> String` (`S: AsRef<str>`) — join fields with `,`, quoting a field (and doubling
  its `"`) when it contains `,`, `"`, `\r` or `\n`. `parse_line(&write_record(f)) == f` for fields
  without line breaks.
* `Table::parse(&str) -> Result<Table, CsvError>` — `header`, `rows`, and `lines` (the line number of
  every row); `CsvError::Empty` if there is no header; `CsvError::Line { line, error }` for a bad line.
  `Table::column(name)` finds a column by exact name.
* `stats::column_sum(&Table, column) -> Result<f64, StatsError>` (feature `stats`) — the sum of the
  column's cells parsed as `f64` after trimming blanks. **Empty cells (after trimming) are skipped.** A
  cell that is not a number is `StatsError::NotANumber { line, value }` with the cell as written.

## CLI (`csvtool`)

```
csvtool count FILE                     number of data records
csvtool columns FILE                   the header names, one per line
csvtool sum FILE COLUMN                column_sum of COLUMN, printed with Rust's `{}` for f64 (7.0 -> 7)
csvtool select FILE COL[,COL...]       a CSV with only those columns, in that order (header included),
                                       written with write_record, one record per line
```

`FILE` may be `-` for standard input. Output ends with a newline.

Errors go to standard error as `csvtool: <message>` with exit status 1:

| situation | message |
|---|---|
| file cannot be read | `cannot read FILE: <io error>` |
| no header | `empty input (no header line)` |
| bad quoting | `line N: unterminated quoted field` / `line N: unexpected text after a closing quote` |
| wrong field count | `line N: expected K fields, found M` |
| unknown column | `no column named "NAME"` |
| non-numeric cell in `sum` | `line N: not a number: "VALUE"` |

A wrong command or a wrong number of arguments prints the usage text (starting with `usage:`) to
standard error and exits with status 2.
