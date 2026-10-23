use std::fmt;

/// Why a single line could not be split into fields.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum LineError {
    /// A quoted field has no closing quote.
    UnterminatedQuote,
    /// Something other than `,` or the end of the line follows a closing quote.
    TextAfterQuote,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum CsvError {
    /// The input has no header line.
    Empty,
    Line { line: usize, error: LineError },
    FieldCount { line: usize, expected: usize, found: usize },
}

impl fmt::Display for CsvError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            CsvError::Empty => write!(f, "empty input (no header line)"),
            CsvError::Line { line, error: LineError::UnterminatedQuote } => {
                write!(f, "line {line}: unterminated quoted field")
            }
            CsvError::Line { line, error: LineError::TextAfterQuote } => {
                write!(f, "line {line}: unexpected text after a closing quote")
            }
            CsvError::FieldCount { line, expected, found } => {
                write!(f, "line {line}: expected {expected} fields, found {found}")
            }
        }
    }
}

impl std::error::Error for CsvError {}

/// Split one line into fields.
pub fn parse_line(line: &str) -> Result<Vec<String>, LineError> {
    let mut fields = Vec::new();
    let mut chars = line.chars().peekable();
    loop {
        let mut field = String::new();
        if chars.peek() == Some(&'"') {
            chars.next();
            loop {
                match chars.next() {
                    None => return Err(LineError::UnterminatedQuote),
                    Some('"') => break,
                    Some(c) => field.push(c),
                }
            }
            match chars.next() {
                None => {
                    fields.push(field);
                    return Ok(fields);
                }
                Some(',') => {
                    fields.push(field);
                    continue;
                }
                Some(_) => return Err(LineError::TextAfterQuote),
            }
        }
        loop {
            match chars.next() {
                None => {
                    fields.push(field);
                    return Ok(fields);
                }
                Some(',') => {
                    fields.push(field);
                    break;
                }
                Some(c) => field.push(c),
            }
        }
    }
}

/// Join fields into one CSV line (no line ending), quoting where needed.
pub fn write_record<S: AsRef<str>>(fields: &[S]) -> String {
    let mut out = String::new();
    for (i, f) in fields.iter().enumerate() {
        let f = f.as_ref();
        if i > 0 {
            out.push(',');
        }
        if f.contains([',', '"', '\n', '\r']) {
            out.push('"');
            out.push_str(&f.replace('"', "\"\""));
            out.push('"');
        } else {
            out.push_str(f);
        }
    }
    out
}

/// A parsed file: the header, the data rows and the 1-based line number of every row.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Table {
    pub header: Vec<String>,
    pub rows: Vec<Vec<String>>,
    pub lines: Vec<usize>,
}

impl Table {
    pub fn parse(text: &str) -> Result<Table, CsvError> {
        let mut header: Option<Vec<String>> = None;
        let mut rows = Vec::new();
        let mut lines = Vec::new();
        for (i, raw) in text.lines().enumerate() {
            let line = i + 1;
            if raw.trim().is_empty() {
                continue;
            }
            let fields = parse_line(raw).map_err(|error| CsvError::Line { line, error })?;
            match &header {
                None => header = Some(fields),
                Some(h) => {
                    if fields.len() != h.len() {
                        return Err(CsvError::FieldCount { line, expected: h.len(), found: fields.len() });
                    }
                    rows.push(fields);
                    lines.push(line);
                }
            }
        }
        let header = header.ok_or(CsvError::Empty)?;
        Ok(Table { header, rows, lines })
    }

    /// Index of the column called `name` (exact match).
    pub fn column(&self, name: &str) -> Option<usize> {
        self.header.iter().position(|h| h == name)
    }
}
