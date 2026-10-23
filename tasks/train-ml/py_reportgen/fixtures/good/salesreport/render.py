from decimal import ROUND_HALF_UP, Decimal

from .aggregate import totals

NUMERIC = ("units", "revenue", "share")


def fmt_money(amount: Decimal) -> str:
    return str(amount.quantize(Decimal("0.01"), rounding=ROUND_HALF_UP))


def table(keys, groups):
    """Header, data rows and TOTAL row as lists of cell strings."""
    header = [*keys, *NUMERIC]
    rows = [[*g.keys, str(g.units), fmt_money(g.revenue), f"{g.share}%"] for g in groups]
    units, revenue = totals(groups)
    total = ["TOTAL", *[""] * (len(keys) - 1), str(units), fmt_money(revenue), "100.0%"]
    return header, rows, total


def render_text(keys, groups) -> str:
    header, rows, total = table(keys, groups)
    n = len(keys)
    widths = [max(len(r[i]) for r in [header, *rows, total]) for i in range(len(header))]

    def line(cells):
        parts = [c.ljust(w) if i < n else c.rjust(w) for i, (c, w) in enumerate(zip(cells, widths))]
        return "  ".join(parts).rstrip()

    dashes = "  ".join("-" * w for w in widths)
    return "\n".join([line(header), dashes, *(line(r) for r in rows), dashes, line(total)])


def render_markdown(keys, groups) -> str:
    header, rows, total = table(keys, groups)
    align = ["---"] * len(keys) + ["---:"] * len(NUMERIC)
    return "\n".join("| " + " | ".join(cells) + " |" for cells in [header, align, *rows, total])


RENDERERS = {"text": render_text, "markdown": render_markdown}
