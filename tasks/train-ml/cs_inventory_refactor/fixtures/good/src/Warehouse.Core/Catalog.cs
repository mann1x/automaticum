namespace Warehouse.Core;

public sealed class Catalog
{
    private readonly Dictionary<string, Product> _products = new(StringComparer.Ordinal);

    public IReadOnlyCollection<Product> All => _products.Values;

    public void Add(Product product)
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

    public bool Contains(string sku) => _products.ContainsKey(sku);

    public Product Get(string sku)
    {
        if (!_products.TryGetValue(sku, out var product))
        {
            throw new KeyNotFoundException($"unknown SKU {sku}");
        }

        return product;
    }
}
