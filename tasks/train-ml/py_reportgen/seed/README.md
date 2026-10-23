# salesreport

Summarises a sales CSV export into a small table. Standard library only (Python 3.12).

```
python3 -m salesreport FILE [--group-by KEYS] [--since YYYY-MM-DD] [--until YYYY-MM-DD]
                            [--format text|markdown]
```

Run the unit tests with `python3 -m unittest discover -s tests -t .`.

## Input

A CSV file with a header row containing (in any order) `date,region,product,channel,units,revenue`.
`date` is `YYYY-MM-DD`, `units` an integer, `revenue` a decimal amount. Blank lines are skipped.
A file that cannot be read, or a malformed row, prints `salesreport: <reason>` on stderr and exits 1.

## Report (v0.3)

### Filtering

`--since` and `--until` are both **inclusive**: `--since 2024-02-01 --until 2024-02-29` keeps every
record dated from Feb 1 to Feb 29, both days included. Either may be omitted.

If no record is left after filtering, the tool prints `no matching records` and exits 0.

### Grouping

`--group-by` takes one or more comma-separated keys, e.g. `--group-by region,product`.
The default is `region`. Valid keys:

| key | groups by |
|---|---|
| `region`, `product`, `channel` | that column's value |
| `month` | the record's month, written `YYYY-MM` |

An unknown key is a usage error: the tool prints `unknown group key: <key>` on stderr (argparse
style) and exits 2.

Records with the same value for every key form one row. Each row has, after its key columns:

- `units` -- the sum of units;
- `revenue` -- the sum of revenue, written with exactly 2 decimals (half-up);
- `share` -- the row's revenue as a percentage of the total revenue of all reported rows, rounded
  half-up to 1 decimal and written with a `%` sign (`6.25` -> `6.3%`). If the total revenue is zero
  every share is `0.0%`.

Rows are sorted by revenue, highest first. Rows with equal revenue are ordered by their key values,
compared key by key, ascending.

After the rows comes a `TOTAL` row: the first key column holds `TOTAL`, the other key columns are
empty, then the total units, total revenue and `100.0%`.

### Text format (default)

```
region  product  units  revenue   share
------  -------  -----  -------  ------
south   gadget       4   100.00   40.0%
north   widget       8    80.00   32.0%
------  -------  -----  -------  ------
TOTAL               12   180.00  100.0%
```

- The header row holds the key names then `units`, `revenue`, `share`.
- Each column is as wide as its widest cell (header, rows and TOTAL row included).
- Key columns are left-aligned; `units`, `revenue` and `share` are right-aligned.
- Columns are separated by two spaces; trailing spaces are removed from every line.
- A line of dashes (one dash per column width, separated by two spaces) follows the header and
  precedes the TOTAL row.

(The example's share figures are illustrative; it shows two of the rows of a larger report.)

### Markdown format (`--format markdown`)

```
| region | product | units | revenue | share |
| --- | --- | ---: | ---: | ---: |
| south | gadget | 4 | 100.00 | 40.0% |
| north | widget | 8 | 80.00 | 32.0% |
| TOTAL |  | 12 | 180.00 | 100.0% |
```

Every line is `"| " + " | ".join(cells) + " |"` with no padding (the alignment
row's cells are `---` for each key column and `---:` for each numeric column). There is no dash line
before the TOTAL row.
