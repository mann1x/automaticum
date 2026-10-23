using Warehouse.Core;

var desk = new WarehouseManager();
desk.AddProduct(new Product("BOLT-M8", "M8 hex bolt", 0.35m));
desk.AddProduct(new Product("NUT-M8", "M8 nut", 0.12m));
desk.AddProduct(new Product("DRILL-10", "10 mm drill bit", 7.90m));
desk.Receive("BOLT-M8", 400);
desk.Receive("NUT-M8", 250);
desk.Receive("DRILL-10", 3);

var result = desk.PlaceOrder(new Order("SO-1001", "Fixit GmbH", new[]
{
    new OrderLine("BOLT-M8", 120),
    new OrderLine("NUT-M8", 120),
    new OrderLine("DRILL-10", 5),
}));

Console.WriteLine($"{result.OrderId}: {result.Status}, total {result.Total:0.00}");
foreach (var line in result.Lines)
{
    Console.WriteLine($"  {line.Sku,-10} {line.Shipped,4}/{line.Requested,-4} {line.Total,8:0.00}");
}

Console.WriteLine($"low stock (< 10): {string.Join(", ", desk.LowStock(10))}");
Console.WriteLine($"stock value: {desk.StockValuation():0.00}");
