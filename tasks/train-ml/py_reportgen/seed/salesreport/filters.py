from datetime import date


def in_range(records, since: date | None = None, until: date | None = None):
    """Keep the records dated between `since` and `until`."""
    out = []
    for r in records:
        if since is not None and r.day < since:
            continue
        if until is not None and r.day >= until:
            continue
        out.append(r)
    return out
