import argparse
import sys
from datetime import date

from .aggregate import group_by, parse_keys
from .filters import in_range
from .records import RecordError, load_records
from .render import RENDERERS


def parse_date(text: str) -> date:
    try:
        return date.fromisoformat(text)
    except ValueError:
        raise argparse.ArgumentTypeError(f"not a date (YYYY-MM-DD): {text!r}") from None


def build_parser() -> argparse.ArgumentParser:
    p = argparse.ArgumentParser(prog="salesreport", description="Summarise a sales CSV export.")
    p.add_argument("csv_file", help="CSV export with date,region,product,channel,units,revenue")
    p.add_argument("--group-by", default="region",
                   help="comma-separated keys: region, product, channel, month (default: region)")
    p.add_argument("--since", type=parse_date, help="first day to include (YYYY-MM-DD)")
    p.add_argument("--until", type=parse_date, help="last day to include (YYYY-MM-DD)")
    p.add_argument("--format", choices=sorted(RENDERERS), default="text", help="output format")
    return p


def main(argv=None) -> int:
    parser = build_parser()
    args = parser.parse_args(argv)
    try:
        keys = parse_keys(args.group_by)
    except ValueError as e:
        parser.error(str(e))
    try:
        records = load_records(args.csv_file)
    except (OSError, RecordError) as e:
        print(f"salesreport: {e}", file=sys.stderr)
        return 1
    records = in_range(records, args.since, args.until)
    if not records:
        print("no matching records")
        return 0
    print(RENDERERS[args.format](keys, group_by(records, keys)))
    return 0
