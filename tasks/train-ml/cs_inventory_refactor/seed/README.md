# Warehouse.Core

Stock and order handling for the warehouse desk (.NET 8, no NuGet packages).

```
src/Warehouse.Core   the library (namespace Warehouse.Core)
src/Warehouse.Demo   a small console demo:  dotnet build Warehouse.sln && dotnet src/Warehouse.Demo/bin/Debug/net8.0/Warehouse.Demo.dll
```

## Current behaviour (keep it)

* **Catalogue.** `AddProduct(Product)`; SKUs are unique and case-sensitive (duplicate or blank SKU:
  `ArgumentException`, negative price: `ArgumentOutOfRangeException`). `GetProduct(sku)` throws
  `KeyNotFoundException` for an unknown SKU.
* **Stock.** `OnHand(sku)` is 0 for anything never received. `Receive`/`Ship` need a catalogue SKU
  (`ArgumentException`) and a positive quantity (`ArgumentOutOfRangeException`); shipping more than is on
  hand throws `InvalidOperationException` and changes nothing.
* **Pricing.** `LineTotal(sku, qty)` = unit price x qty, 5% off from 10 units, 10% off from 50 units,
  rounded to cents (midpoint away from zero).
* **Orders.** `PlaceOrder(Order)` validates the whole order first -- no lines, an unknown SKU or a
  quantity <= 0 throws `ArgumentException` and nothing changes. Line totals use the pricing rule above;
  the order total is the sum of the line totals. Every placed order is appended to `OrderHistory`.
* **Reports.** `LowStock(threshold)`: catalogue SKUs with on-hand strictly below the threshold, ordered by
  on-hand ascending, then SKU (ordinal). `StockValuation()`: sum of unit price x on-hand.

## The refactoring we want

`WarehouseManager` does everything and has become hard to change. Split it into these public types
(all in namespace `Warehouse.Core`):

```csharp
public interface IStockRepository
{
    int OnHand(string sku);                // 0 for a SKU that was never stocked
    void Adjust(string sku, int delta);    // InvalidOperationException (and no change) if the result would be < 0
    IReadOnlyCollection<string> Skus { get; }   // SKUs that have ever been adjusted
}

public sealed class InMemoryStockRepository : IStockRepository { public InMemoryStockRepository(); ... }

public sealed class Catalog
{
    public void Add(Product product);      // same validation as AddProduct
    public Product Get(string sku);        // KeyNotFoundException
    public bool Contains(string sku);
    public IReadOnlyCollection<Product> All { get; }
}

public sealed class PricingService
{
    public PricingService(Catalog catalog);
    public decimal LineTotal(string sku, int quantity);
}

public sealed class OrderProcessor
{
    public OrderProcessor(Catalog catalog, IStockRepository stock, PricingService pricing);
    public OrderResult Place(Order order);
    public IReadOnlyList<OrderResult> History { get; }
}

public sealed class StockReport
{
    public StockReport(Catalog catalog, IStockRepository stock);
    public IReadOnlyList<string> LowStock(int threshold);
    public decimal Valuation();
}
```

The components must only talk to stock through `IStockRepository` (a different repository
implementation must work with `OrderProcessor` and `StockReport`).

`WarehouseManager` stays, with its existing public methods, as a thin facade over those components:

* `new WarehouseManager()` uses a fresh `Catalog` and `InMemoryStockRepository`;
  `new WarehouseManager(Catalog catalog, IStockRepository stock)` uses the ones it is given;
* it exposes them as properties `Catalog`, `Stock` (IStockRepository), `Pricing`, `Orders`
  (the OrderProcessor) and `Reports` (the StockReport);
* it keeps no catalogue, stock or order state of its own.

## The behaviour change: backorders

Orders no longer ship all-or-nothing. After validation, lines are allocated in order: each line ships
`min(requested, on hand at that moment)` and the rest is backordered (two lines for the same SKU compete
for the same stock, first line first). Line totals are priced on the **shipped** quantity (discount
tiers by shipped quantity too); the order total is the sum.

* `LineResult` gets an `int Backordered` (= Requested - Shipped):
  `LineResult(string Sku, int Requested, int Shipped, int Backordered, decimal Total)`.
* `OrderStatus` becomes `Filled` (nothing backordered), `PartiallyFilled` (something shipped, something
  backordered) and `Backordered` (nothing shipped). `Rejected` goes away.
* Invalid orders still throw `ArgumentException` before any stock moves.
