from decimal import ROUND_HALF_EVEN, Decimal


def cents(value) -> Decimal:
    """Round an amount to cents, half even."""
    return Decimal(str(value)).quantize(Decimal("0.01"), rounding=ROUND_HALF_EVEN)
