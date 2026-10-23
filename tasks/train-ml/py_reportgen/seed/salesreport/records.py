import csv
from dataclasses import dataclass
from datetime import date
from decimal import Decimal, InvalidOperation

FIELDS = ("date", "region", "product", "channel", "units", "revenue")


class RecordError(ValueError):
    """A CSV export that cannot be read."""


@dataclass(frozen=True)
class Record:
    day: date
    region: str
    product: str
    channel: str
    units: int
    revenue: Decimal


def parse_row(row: dict, lineno: int) -> Record:
    try:
        return Record(
            day=date.fromisoformat(row["date"].strip()),
            region=row["region"].strip(),
            product=row["product"].strip(),
            channel=row["channel"].strip(),
            units=int(row["units"]),
            revenue=Decimal(row["revenue"].strip()),
        )
    except (KeyError, ValueError, InvalidOperation, AttributeError) as e:
        raise RecordError(f"line {lineno}: {e or type(e).__name__}") from None


def load_records(path) -> list[Record]:
    with open(path, newline="", encoding="utf-8") as fh:
        reader = csv.DictReader(fh)
        missing = [f for f in FIELDS if f not in (reader.fieldnames or [])]
        if missing:
            raise RecordError(f"missing column(s): {', '.join(missing)}")
        records = []
        for lineno, row in enumerate(reader, start=2):
            if not any((v or "").strip() for v in row.values()):
                continue
            records.append(parse_row(row, lineno))
        return records
