import tempfile
import unittest
from datetime import date
from decimal import Decimal
from pathlib import Path

from salesreport import RecordError, load_records


class LoadRecordsTest(unittest.TestCase):
    def write(self, text):
        d = tempfile.TemporaryDirectory()
        self.addCleanup(d.cleanup)
        p = Path(d.name) / "x.csv"
        p.write_text(text)
        return p

    def test_sample_loads(self):
        recs = load_records(Path(__file__).parent.parent / "sample" / "sales.csv")
        self.assertEqual(len(recs), 12)
        self.assertEqual(recs[0].day, date(2024, 1, 3))
        self.assertEqual(recs[0].revenue, Decimal("79.96"))

    def test_blank_lines_skipped(self):
        p = self.write("date,region,product,channel,units,revenue\n2024-01-01,n,w,web,1,2.00\n,,,,,\n")
        self.assertEqual(len(load_records(p)), 1)

    def test_missing_column(self):
        p = self.write("date,region,units,revenue\n2024-01-01,n,1,2.00\n")
        with self.assertRaisesRegex(RecordError, "product"):
            load_records(p)

    def test_bad_number_reports_line(self):
        p = self.write("date,region,product,channel,units,revenue\n2024-01-01,n,w,web,x,2.00\n")
        with self.assertRaisesRegex(RecordError, "line 2"):
            load_records(p)


if __name__ == "__main__":
    unittest.main()
