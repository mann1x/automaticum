from decimal import ROUND_HALF_UP, Decimal


def cents(value) -> Decimal:
    """Round an amount to cents, half up."""
    return Decimal(str(value)).quantize(Decimal("0.01"), rounding=ROUND_HALF_UP)
