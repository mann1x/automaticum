"""salesreport: summarise sales CSV exports into small tables."""

from .records import Record, RecordError, load_records

__all__ = ["Record", "RecordError", "load_records"]
__version__ = "0.2.1"
