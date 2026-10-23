use csvcore::{parse_line, write_record, Table};

#[test]
fn splits_simple_lines() {
    assert_eq!(parse_line("a,b,,c").unwrap(), vec!["a", "b", "", "c"]);
    assert_eq!(parse_line("\"x, y\",z").unwrap(), vec!["x, y", "z"]);
}

#[test]
fn parses_a_table() {
    let t = Table::parse("name,qty\napple,3\n\npear,5\n").unwrap();
    assert_eq!(t.header, vec!["name", "qty"]);
    assert_eq!(t.rows.len(), 2);
    assert_eq!(t.lines, vec![2, 4]);
    assert_eq!(t.column("qty"), Some(1));
}

#[test]
fn writes_records() {
    assert_eq!(write_record(&["a", "b,c"]), "a,\"b,c\"");
}
