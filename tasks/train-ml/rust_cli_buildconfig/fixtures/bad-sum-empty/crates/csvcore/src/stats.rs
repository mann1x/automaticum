use std::fmt;

use crate::reader::Table;

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum StatsError {
    NotANumber { line: usize, value: String },
}

impl fmt::Display for StatsError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            StatsError::NotANumber { line, value } => write!(f, "line {line}: not a number: {value:?}"),
        }
    }
}

impl std::error::Error for StatsError {}

/// Sum of a numeric column (see the README for which cells count).
pub fn column_sum(table: &Table, column: usize) -> Result<f64, StatsError> {
    let mut sum = 0.0;
    for (row, &line) in table.rows.iter().zip(&table.lines) {
        let cell = row[column].trim();
        let v: f64 = cell
            .parse()
            .map_err(|_| StatsError::NotANumber { line, value: row[column].clone() })?;
        sum += v;
    }
    Ok(sum)
}
