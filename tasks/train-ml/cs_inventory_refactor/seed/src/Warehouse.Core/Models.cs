namespace Warehouse.Core;

public sealed record Product(string Sku, string Name, decimal UnitPrice);

public sealed record OrderLine(string Sku, int Quantity);

public sealed record Order(string Id, string Customer, IReadOnlyList<OrderLine> Lines);

public enum OrderStatus
{
    Filled,
    Rejected,
}

public sealed record LineResult(string Sku, int Requested, int Shipped, decimal Total);

public sealed record OrderResult(string OrderId, OrderStatus Status, IReadOnlyList<LineResult> Lines, decimal Total);
