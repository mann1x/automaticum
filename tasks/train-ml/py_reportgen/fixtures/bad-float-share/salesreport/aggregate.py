from dataclasses import dataclass
from decimal import Decimal

GROUP_KEYS = ("region", "product", "channel", "month")


@dataclass
class Group:
    keys: tuple[str, ...]
    units: int = 0
    revenue: Decimal = Decimal("0")
    share: Decimal = Decimal("0.0")


def key_value(record, key: str) -> str:
    if key == "month":
        return f"{record.day.year:04d}-{record.day.month:02d}"
    return getattr(record, key)


def parse_keys(text: str) -> list[str]:
    keys = [k.strip() for k in text.split(",")]
    for k in keys:
        if k not in GROUP_KEYS:
            raise ValueError(f"unknown group key: {k}")
    return keys


def percent(part: Decimal, whole: Decimal) -> Decimal:
    if whole == 0:
        return Decimal("0.0")
    return Decimal(f"{float(part) * 100 / float(whole):.1f}")


def group_by(records, keys: list[str]) -> list[Group]:
    groups: dict[tuple[str, ...], Group] = {}
    for r in records:
        k = tuple(key_value(r, key) for key in keys)
        g = groups.setdefault(k, Group(k))
        g.units += r.units
        g.revenue += r.revenue
    total = sum((g.revenue for g in groups.values()), Decimal("0"))
    for g in groups.values():
        g.share = percent(g.revenue, total)
    rows = sorted(groups.values(), key=lambda g: g.keys)
    rows.sort(key=lambda g: g.revenue, reverse=True)
    return rows


def totals(groups) -> tuple[int, Decimal]:
    return sum(g.units for g in groups), sum((g.revenue for g in groups), Decimal("0"))
