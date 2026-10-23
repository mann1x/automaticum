from decimal import Decimal

from .money import cents


class Invoice:
    def __init__(self, tax_rate="0", percent_off="0", fixed_off="0"):
        self.tax_rate = Decimal(str(tax_rate))
        self.percent_off = Decimal(str(percent_off))
        self.fixed_off = Decimal(str(fixed_off))
        self.lines = []

    def add_line(self, description, qty, unit_price):
        if isinstance(qty, bool) or not isinstance(qty, int) or qty <= 0:
            raise ValueError(f"quantity must be a positive integer, got {qty!r}")
        self.lines.append((description, qty, Decimal(str(unit_price))))

    def line_totals(self):
        return [cents(price * qty) for _, qty, price in self.lines]

    def subtotal(self):
        return sum(self.line_totals(), Decimal("0"))

    def summary(self):
        subtotal = self.subtotal()
        taxable = subtotal - cents(subtotal * self.percent_off / 100)
        taxable = max(taxable - self.fixed_off, Decimal("0.00"))
        tax = cents(taxable * self.tax_rate / 100)
        return {
            "subtotal": subtotal,
            "discount": subtotal - taxable,
            "taxable": taxable,
            "tax": tax,
            "total": taxable + tax,
        }
