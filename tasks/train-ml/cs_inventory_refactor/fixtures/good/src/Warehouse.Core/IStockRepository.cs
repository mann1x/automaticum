namespace Warehouse.Core;

public interface IStockRepository
{
    int OnHand(string sku);

    void Adjust(string sku, int delta);

    IReadOnlyCollection<string> Skus { get; }
}
