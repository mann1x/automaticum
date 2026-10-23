# cs_inventory_refactor notes

- Seed: `WarehouseManager` holds catalogue, stock, pricing, all-or-nothing order processing and reports in one
  class (dictionaries + history list). README specifies the target types (IStockRepository,
  InMemoryStockRepository, Catalog, PricingService, OrderProcessor, StockReport), the facade wiring, and the
  one behaviour change: backorders (ship min(requested, on hand) per line in order, price on shipped qty,
  LineResult.Backordered, OrderStatus Filled/PartiallyFilled/Backordered, Rejected removed).
- Reference (`fixtures/good`): one file per type; the facade builds the components from (Catalog,
  IStockRepository) and only delegates.
- The checker is reflection + `dynamic` only (compiles against nothing but the assembly name), so a missing
  type/member is a named test failure ("public type Warehouse.Core.StockReport not found",
  RuntimeBinderException text), never a hidden-test compile error. `components_standalone` hands
  OrderProcessor/StockReport a DispatchProxy IStockRepository, so a processor that bypasses the interface fails;
  `facade_wiring` fails a facade that still holds collection-typed fields.
- Near-misses: `priced-on-requested` (prices backordered lines on the requested qty) and
  `facade-keeps-history` (facade keeps its own history list).
- .NET offline recipe as cs_buildfix (NuGet migrations marker under `__checker__/home`, no packages,
  `dotnet x.dll`): passes verify both with the default exec /tmp and with `--tmp-noexec`.
