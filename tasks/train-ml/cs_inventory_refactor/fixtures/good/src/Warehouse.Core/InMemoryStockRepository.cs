namespace Warehouse.Core;

public sealed class InMemoryStockRepository : IStockRepository
{
    private readonly Dictionary<string, int> _onHand = new(StringComparer.Ordinal);

    public IReadOnlyCollection<string> Skus => _onHand.Keys;

    public int OnHand(string sku) => _onHand.TryGetValue(sku, out var qty) ? qty : 0;

    public void Adjust(string sku, int delta)
    {
        var next = OnHand(sku) + delta;
        if (next < 0)
        {
            throw new InvalidOperationException($"stock of {sku} would drop to {next}");
        }

        _onHand[sku] = next;
    }
}
