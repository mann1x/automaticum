namespace Warehouse.Core;

/// <summary>Facade over the warehouse components, kept for existing callers.</summary>
public class WarehouseManager
{
    public WarehouseManager()
        : this(new Catalog(), new InMemoryStockRepository())
    {
    }

    public WarehouseManager(Catalog catalog, IStockRepository stock)
    {
        Catalog = catalog ?? throw new ArgumentNullException(nameof(catalog));
        Stock = stock ?? throw new ArgumentNullException(nameof(stock));
        Pricing = new PricingService(catalog);
        Orders = new OrderProcessor(catalog, stock, Pricing);
        Reports = new StockReport(catalog, stock);
    }

    public Catalog Catalog { get; }

    public IStockRepository Stock { get; }

    public PricingService Pricing { get; }

    public OrderProcessor Orders { get; }

    public StockReport Reports { get; }

    public IReadOnlyList<OrderResult> OrderHistory => Orders.History;

    public void AddProduct(Product product) => Catalog.Add(product);

    public Product GetProduct(string sku) => Catalog.Get(sku);

    public int OnHand(string sku) => Stock.OnHand(sku);

    public void Receive(string sku, int quantity)
    {
        RequireStockMove(sku, quantity);
        Stock.Adjust(sku, quantity);
    }

    public void Ship(string sku, int quantity)
    {
        RequireStockMove(sku, quantity);
        var available = Stock.OnHand(sku);
        if (available < quantity)
        {
            throw new InvalidOperationException($"cannot ship {quantity} x {sku}: only {available} on hand");
        }

        Stock.Adjust(sku, -quantity);
    }

    public decimal LineTotal(string sku, int quantity) => Pricing.LineTotal(sku, quantity);

    public OrderResult PlaceOrder(Order order) => Orders.Place(order);

    public IReadOnlyList<string> LowStock(int threshold) => Reports.LowStock(threshold);

    public decimal StockValuation() => Reports.Valuation();

    private void RequireStockMove(string sku, int quantity)
    {
        if (!Catalog.Contains(sku))
        {
            throw new ArgumentException($"unknown SKU {sku}", nameof(sku));
        }

        if (quantity <= 0)
        {
            throw new ArgumentOutOfRangeException(nameof(quantity), quantity, "quantity must be positive");
        }
    }
}
