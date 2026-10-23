from decimal import ROUND_HALF_UP, Decimal


def fmt_money(amount: Decimal) -> str:
    return str(amount.quantize(Decimal("0.01"), rounding=ROUND_HALF_UP))


def render_text(key: str, groups) -> str:
    header = [key, "units", "revenue"]
    rows = [[g.key, str(g.units), fmt_money(g.revenue)] for g in groups]
    widths = [max(len(r[i]) for r in [header, *rows]) for i in range(len(header))]

    def line(cells):
        parts = [cells[0].ljust(widths[0])]
        parts += [c.rjust(w) for c, w in zip(cells[1:], widths[1:])]
        return "  ".join(parts).rstrip()

    out = [line(header), "  ".join("-" * w for w in widths)]
    out += [line(r) for r in rows]
    return "\n".join(out)
