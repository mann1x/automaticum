import sys
import traceback
from decimal import Decimal as D

sys.path.insert(0, ".")


def case(name, fn):
    try:
        fn()
        print(f"TEST {name} PASS")
    except AssertionError as e:
        print(f"TEST {name} FAIL {e}")
    except Exception as e:
        frame = traceback.extract_tb(e.__traceback__)[-1]
        where = "" if "__checker__" in frame.filename else f" at {frame.filename.split('/')[-1]}:{frame.lineno}"
        print(f"TEST {name} FAIL {type(e).__name__}: {e}{where}")


def eq(got, want, what):
    assert got == want and type(got) is D, f"{what}: expected Decimal {want}, got {type(got).__name__} {got}"


def t_round_half_up():
    from ledger import cents
    eq(cents(D("0.005")), D("0.01"), "cents(0.005)")
    eq(cents(D("2.675")), D("2.68"), "cents(2.675)")
    eq(cents(D("1.125")), D("1.13"), "cents(1.125)")


def t_no_float_error():
    from ledger import cents
    eq(cents(D("1234567.895")), D("1234567.90"), "cents(1234567.895)")


def t_line_rounding():
    from ledger import Invoice
    inv = Invoice()
    inv.add_line("bolts", 3, "0.335")
    eq(inv.subtotal(), D("1.01"), "subtotal of 3 x 0.335")


def t_qty_validation():
    from ledger import Invoice
    for bad in (0, -2, 1.5, "3", True):
        try:
            Invoice().add_line("x", bad, "1.00")
        except ValueError:
            continue
        raise AssertionError(f"add_line with quantity {bad!r} did not raise ValueError")


def t_discount_before_tax():
    from ledger import Invoice
    inv = Invoice(tax_rate="20", percent_off="10")
    inv.add_line("desk", 1, "100.00")
    s = inv.summary()
    eq(s["taxable"], D("90.00"), "taxable")
    eq(s["tax"], D("18.00"), "tax")
    eq(s["total"], D("108.00"), "total")
    eq(s["discount"], D("10.00"), "discount")


def t_fractional_percent():
    from ledger import Invoice
    inv = Invoice(percent_off="12.5")
    inv.add_line("chair", 2, "40.00")
    eq(inv.summary()["discount"], D("10.00"), "12.5% of 80.00")


def t_fixed_after_percent():
    from ledger import Invoice
    inv = Invoice(tax_rate="10", percent_off="50", fixed_off="5")
    inv.add_line("lamp", 1, "30.00")
    s = inv.summary()
    eq(s["taxable"], D("10.00"), "taxable (30 - 50% - 5)")
    eq(s["total"], D("11.00"), "total")


def t_never_negative():
    from ledger import Invoice
    inv = Invoice(tax_rate="10", fixed_off="50")
    inv.add_line("pen", 2, "1.50")
    s = inv.summary()
    eq(s["taxable"], D("0.00"), "taxable")
    eq(s["total"], D("0.00"), "total")
    eq(s["discount"], D("3.00"), "discount")


def t_mixed_invoice():
    from ledger import Invoice
    inv = Invoice(tax_rate="7.25", percent_off="3", fixed_off="1.10")
    inv.add_line("a", 7, "1.99")
    inv.add_line("b", 1, "0.015")
    inv.add_line("c", 12, "0.125")
    s = inv.summary()
    eq(s["subtotal"], D("15.45"), "subtotal")
    eq(s["taxable"], D("13.89"), "taxable")
    eq(s["tax"], D("1.01"), "tax")
    eq(s["total"], D("14.90"), "total")


for name, fn in [("round_half_up", t_round_half_up), ("no_float_error", t_no_float_error),
                 ("line_rounding", t_line_rounding), ("qty_validation", t_qty_validation),
                 ("discount_before_tax", t_discount_before_tax), ("fractional_percent", t_fractional_percent),
                 ("fixed_after_percent", t_fixed_after_percent), ("never_negative", t_never_negative),
                 ("mixed_invoice", t_mixed_invoice)]:
    case(name, fn)
