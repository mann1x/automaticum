from dataclasses import dataclass
from decimal import Decimal

GROUP_KEYS = ("region", "product", "channel")


@dataclass
class Group:
    key: str
    units: int = 0
    revenue: Decimal = Decimal("0")


def group_by(records, key: str) -> list[Group]:
    if key not in GROUP_KEYS:
        raise ValueError(f"cannot group by {key}")
    groups: dict[str, Group] = {}
    for r in records:
        k = getattr(r, key)
        g = groups.setdefault(k, Group(k))
        g.units += r.units
        g.revenue += r.revenue
    return sorted(groups.values(), key=lambda g: g.key)
