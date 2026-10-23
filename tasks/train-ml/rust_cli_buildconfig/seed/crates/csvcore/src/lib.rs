//! csvcore: a small CSV reader/writer (records are lines; see the workspace README).

pub mod reader;
#[cfg(feature = "stats")]
pub mod stats;

pub use reader::{parse_line, write_record, CsvError, LineError, Table};
