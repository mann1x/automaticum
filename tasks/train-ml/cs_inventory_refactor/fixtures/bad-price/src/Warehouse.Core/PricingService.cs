namespace Warehouse.Core;

public sealed class PricingService
{
    private readonly Catalog _catalog;

    public PricingService(Catalog catalog)
    {
        _catalog = catalog ?? throw new ArgumentNullException(nameof(catalog));
    }

    public decimal LineTotal(string sku, int quantity)
    {
        var product = _catalog.Get(sku);
        if (quantity < 0)
        {
            throw new ArgumentOutOfRangeException(nameof(quantity), quantity, "quantity is negative");
        }

        var gross = product.UnitPrice * quantity;
        var discount = quantity >= 50 ? 0.10m : quantity >= 10 ? 0.05m : 0m;
        return Math.Round(gross * (1 - discount), 2, MidpointRounding.AwayFromZero);
    }
}
