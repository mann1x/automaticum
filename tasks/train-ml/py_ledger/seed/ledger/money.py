from decimal import Decimal


def cents(value) -> Decimal:
    """Round an amount to cents."""
    return Decimal(str(round(float(value), 2)))
