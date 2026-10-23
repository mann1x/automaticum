# ledger

Small invoicing library. All amounts are `decimal.Decimal`; floats are never used.

## Rules

1. **Money** is rounded to cents with ROUND_HALF_UP (`Decimal("0.005")` -> `0.01`).
2. **Line total** = unit price x quantity, rounded to cents per line.
   Quantity must be a positive integer; anything else raises `ValueError`.
3. **Subtotal** = sum of line totals.
4. **Discounts** are applied to the subtotal *before* tax, in this order:
   1. the percentage discount (any decimal percentage, e.g. `12.5`), rounded to cents;
   2. then the fixed discount.
   The discounted subtotal never goes below zero.
5. **Tax** = discounted subtotal x tax rate (a percentage), rounded to cents.
6. **Total** = discounted subtotal + tax.

`Invoice.summary()` returns a dict with the keys `subtotal`, `discount`, `taxable`, `tax`, `total`
(all `Decimal`), where `discount` is the total amount taken off.
