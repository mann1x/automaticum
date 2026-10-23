from decimal import Decimal

from .money import cents


class Invoice:
    def __init__(self, tax_rate="0", percent_off="0", fixed_off="0"):
        self.tax_rate = Decimal(str(tax_rate))
        self.percent_off = int(Decimal(str(percent_off)))
        self.fixed_off = Decimal(str(fixed_off))
        self.lines = []

    def add_line(self, description, qty, unit_price):
        self.lines.append((description, qty, Decimal(str(unit_price))))

    def line_totals(self):
        return [cents(price * qty) for _, qty, price in self.lines]

    def subtotal(self):
        return sum(self.line_totals(), Decimal("0"))

    def summary(self):
        subtotal = self.subtotal()
        tax = cents(subtotal * self.tax_rate / 100)
        gross = subtotal + tax
        discount = cents(gross * self.percent_off / 100) + self.fixed_off
        total = gross - discount
        return {
            "subtotal": subtotal,
            "discount": discount,
            "taxable": subtotal,
            "tax": tax,
            "total": total,
        }
