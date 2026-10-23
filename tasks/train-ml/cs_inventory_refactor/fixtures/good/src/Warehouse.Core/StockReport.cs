namespace Warehouse.Core;

public sealed class StockReport
{
    private readonly Catalog _catalog;
    private readonly IStockRepository _stock;

    public StockReport(Catalog catalog, IStockRepository stock)
    {
        _catalog = catalog ?? throw new ArgumentNullException(nameof(catalog));
        _stock = stock ?? throw new ArgumentNullException(nameof(stock));
    }

    public IReadOnlyList<string> LowStock(int threshold)
    {
        return _catalog.All
            .Select(p => p.Sku)
            .Where(sku => _stock.OnHand(sku) < threshold)
            .OrderBy(_stock.OnHand)
            .ThenBy(sku => sku, StringComparer.Ordinal)
            .ToList();
    }

    public decimal Valuation() => _catalog.All.Sum(p => p.UnitPrice * _stock.OnHand(p.Sku));
}
