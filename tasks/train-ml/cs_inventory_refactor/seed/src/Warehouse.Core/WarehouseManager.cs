namespace Warehouse.Core;

/// <summary>
/// Everything the warehouse desk needs: the product catalogue, stock levels, prices, order
/// processing and the stock reports.
/// </summary>
public class WarehouseManager
{
    private readonly Dictionary<string, Product> _products = new(StringComparer.Ordinal);
    private readonly Dictionary<string, int> _onHand = new(StringComparer.Ordinal);
    private readonly List<OrderResult> _history = new();

    public IReadOnlyList<OrderResult> OrderHistory => _history;

    public void AddProduct(Product product)
    {
        ArgumentNullException.ThrowIfNull(product);
        if (string.IsNullOrWhiteSpace(product.Sku))
        {
            throw new ArgumentException("product has no SKU", nameof(product));
        }

        if (product.UnitPrice < 0)
        {
            throw new ArgumentOutOfRangeException(nameof(product), product.UnitPrice, "unit price is negative");
        }

        if (!_products.TryAdd(product.Sku, product))
        {
            throw new ArgumentException($"duplicate SKU {product.Sku}", nameof(product));
        }
    }

    public Product GetProduct(string sku)
    {
        if (!_products.TryGetValue(sku, out var product))
        {
            throw new KeyNotFoundException($"unknown SKU {sku}");
        }

        return product;
    }

    public int OnHand(string sku)
    {
        return _onHand.TryGetValue(sku, out var qty) ? qty : 0;
    }

    public void Receive(string sku, int quantity)
    {
        if (!_products.ContainsKey(sku))
        {
            throw new ArgumentException($"unknown SKU {sku}", nameof(sku));
        }

        if (quantity <= 0)
        {
            throw new ArgumentOutOfRangeException(nameof(quantity), quantity, "quantity must be positive");
        }

        _onHand[sku] = OnHand(sku) + quantity;
    }

    public void Ship(string sku, int quantity)
    {
        if (!_products.ContainsKey(sku))
        {
            throw new ArgumentException($"unknown SKU {sku}", nameof(sku));
        }

        if (quantity <= 0)
        {
            throw new ArgumentOutOfRangeException(nameof(quantity), quantity, "quantity must be positive");
        }

        var available = OnHand(sku);
        if (available < quantity)
        {
            throw new InvalidOperationException($"cannot ship {quantity} x {sku}: only {available} on hand");
        }

        _onHand[sku] = available - quantity;
    }

    public decimal LineTotal(string sku, int quantity)
    {
        var product = GetProduct(sku);
        if (quantity < 0)
        {
            throw new ArgumentOutOfRangeException(nameof(quantity), quantity, "quantity is negative");
        }

        var gross = product.UnitPrice * quantity;
        var discount = quantity >= 50 ? 0.10m : quantity >= 10 ? 0.05m : 0m;
        return Math.Round(gross * (1 - discount), 2, MidpointRounding.AwayFromZero);
    }

    public OrderResult PlaceOrder(Order order)
    {
        ArgumentNullException.ThrowIfNull(order);
        if (order.Lines.Count == 0)
        {
            throw new ArgumentException($"order {order.Id} has no lines", nameof(order));
        }

        foreach (var line in order.Lines)
        {
            if (!_products.ContainsKey(line.Sku))
            {
                throw new ArgumentException($"order {order.Id}: unknown SKU {line.Sku}", nameof(order));
            }

            if (line.Quantity <= 0)
            {
                throw new ArgumentException($"order {order.Id}: quantity of {line.Sku} must be positive", nameof(order));
            }
        }

        // An order ships complete or not at all.
        var needed = new Dictionary<string, int>(StringComparer.Ordinal);
        foreach (var line in order.Lines)
        {
            needed[line.Sku] = needed.GetValueOrDefault(line.Sku) + line.Quantity;
        }

        OrderResult result;
        if (needed.Any(kv => OnHand(kv.Key) < kv.Value))
        {
            var rejected = order.Lines.Select(l => new LineResult(l.Sku, l.Quantity, 0, 0m)).ToList();
            result = new OrderResult(order.Id, OrderStatus.Rejected, rejected, 0m);
        }
        else
        {
            var lines = new List<LineResult>();
            foreach (var line in order.Lines)
            {
                _onHand[line.Sku] = OnHand(line.Sku) - line.Quantity;
                lines.Add(new LineResult(line.Sku, line.Quantity, line.Quantity, LineTotal(line.Sku, line.Quantity)));
            }

            result = new OrderResult(order.Id, OrderStatus.Filled, lines, lines.Sum(l => l.Total));
        }

        _history.Add(result);
        return result;
    }

    public IReadOnlyList<string> LowStock(int threshold)
    {
        return _products.Keys
            .Where(sku => OnHand(sku) < threshold)
            .OrderBy(OnHand)
            .ThenBy(sku => sku, StringComparer.Ordinal)
            .ToList();
    }

    public decimal StockValuation()
    {
        return _products.Values.Sum(p => p.UnitPrice * OnHand(p.Sku));
    }
}
