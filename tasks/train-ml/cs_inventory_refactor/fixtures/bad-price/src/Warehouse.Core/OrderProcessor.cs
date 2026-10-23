namespace Warehouse.Core;

public sealed class OrderProcessor
{
    private readonly Catalog _catalog;
    private readonly IStockRepository _stock;
    private readonly PricingService _pricing;
    private readonly List<OrderResult> _history = new();

    public OrderProcessor(Catalog catalog, IStockRepository stock, PricingService pricing)
    {
        _catalog = catalog ?? throw new ArgumentNullException(nameof(catalog));
        _stock = stock ?? throw new ArgumentNullException(nameof(stock));
        _pricing = pricing ?? throw new ArgumentNullException(nameof(pricing));
    }

    public IReadOnlyList<OrderResult> History => _history;

    public OrderResult Place(Order order)
    {
        Validate(order);

        var lines = new List<LineResult>();
        foreach (var line in order.Lines)
        {
            var shipped = Math.Min(line.Quantity, _stock.OnHand(line.Sku));
            if (shipped > 0)
            {
                _stock.Adjust(line.Sku, -shipped);
            }

            lines.Add(new LineResult(line.Sku, line.Quantity, shipped, line.Quantity - shipped, _pricing.LineTotal(line.Sku, line.Quantity)));
        }

        var status = lines.All(l => l.Backordered == 0) ? OrderStatus.Filled
            : lines.All(l => l.Shipped == 0) ? OrderStatus.Backordered
            : OrderStatus.PartiallyFilled;
        var result = new OrderResult(order.Id, status, lines, lines.Sum(l => l.Total));
        _history.Add(result);
        return result;
    }

    private void Validate(Order order)
    {
        ArgumentNullException.ThrowIfNull(order);
        if (order.Lines.Count == 0)
        {
            throw new ArgumentException($"order {order.Id} has no lines", nameof(order));
        }

        foreach (var line in order.Lines)
        {
            if (!_catalog.Contains(line.Sku))
            {
                throw new ArgumentException($"order {order.Id}: unknown SKU {line.Sku}", nameof(order));
            }

            if (line.Quantity <= 0)
            {
                throw new ArgumentException($"order {order.Id}: quantity of {line.Sku} must be positive", nameof(order));
            }
        }
    }
}
