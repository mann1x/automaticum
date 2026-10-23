using System.Collections;
using System.Diagnostics;
using System.Reflection;

public class CountingRepo : DispatchProxy
{
    public object Inner;
    public List<string> Calls = new();

    protected override object Invoke(MethodInfo method, object[] args)
    {
        Calls.Add(method.Name);
        try { return method.Invoke(Inner, args); }
        catch (TargetInvocationException e) { throw e.InnerException; }
    }
}

static class T
{
    sealed class Fail : Exception { public Fail(string m) : base(m) { } }

    static Assembly asm;

    static void Check(bool cond, string msg) { if (!cond) throw new Fail(msg); }

    static void Eq(object got, object want, string what)
    {
        if (!Equals(got, want)) throw new Fail($"{what}: expected {want}, got {got ?? "null"}");
    }

    static void Throws<TE>(Action a, string what) where TE : Exception
    {
        try { a(); }
        catch (TE) { return; }
        catch (Fail) { throw; }
        catch (Exception e) { throw new Fail($"{what}: expected {typeof(TE).Name}, got {e.GetType().Name}: {e.Message}"); }
        throw new Fail($"{what}: expected {typeof(TE).Name}, nothing was thrown");
    }

    static Type Ty(string name)
    {
        var t = asm.GetType("Warehouse.Core." + name);
        if (t == null || !t.IsPublic) throw new Fail($"public type Warehouse.Core.{name} not found");
        return t;
    }

    static dynamic New(string name, params object[] args)
    {
        var t = Ty(name);
        try { return Activator.CreateInstance(t, args); }
        catch (MissingMethodException)
        {
            throw new Fail($"{name} has no public constructor taking ({string.Join(", ", args.Select(a => a?.GetType().Name ?? "null"))})");
        }
        catch (TargetInvocationException e) { throw e.InnerException; }
    }

    static dynamic Ord(string id, params (string Sku, int Qty)[] lines)
    {
        var lt = Ty("OrderLine");
        var arr = Array.CreateInstance(lt, lines.Length);
        for (var i = 0; i < lines.Length; i++) arr.SetValue(Activator.CreateInstance(lt, lines[i].Sku, lines[i].Qty), i);
        return Activator.CreateInstance(Ty("Order"), id, "Fixit GmbH", arr);
    }

    static List<dynamic> Items(object seq)
    {
        var l = new List<dynamic>();
        foreach (var x in (IEnumerable)seq) l.Add(x);
        return l;
    }

    static int RepoOnHand(object repo, string sku) => (int)Call(repo, "OnHand", sku);
    static void RepoAdjust(object repo, string sku, int delta) => Call(repo, "Adjust", sku, delta);

    static object Call(object repo, string method, params object[] args)
    {
        var m = Ty("IStockRepository").GetMethod(method) ?? throw new Fail($"IStockRepository has no method {method}");
        try { return m.Invoke(repo, args); }
        catch (TargetInvocationException e) { throw e.InnerException; }
    }

    static dynamic Desk()
    {
        dynamic m = New("WarehouseManager");
        m.AddProduct(New("Product", "BOLT-M8", "M8 hex bolt", 0.35m));
        m.AddProduct(New("Product", "NUT-M8", "M8 nut", 0.12m));
        m.AddProduct(New("Product", "DRILL-10", "10 mm drill bit", 7.90m));
        return m;
    }

    static void ExpectLine(dynamic line, string sku, int requested, int shipped, int backordered, decimal total)
    {
        string got = $"{line.Sku} req {line.Requested} shipped {line.Shipped} backordered {line.Backordered} total {(decimal)line.Total:0.00}";
        string want = $"{sku} req {requested} shipped {shipped} backordered {backordered} total {total:0.00}";
        Eq(got, want, "order line");
    }

    static void Case(string name, Action fn)
    {
        string msg;
        try { fn(); Console.WriteLine($"TEST {name} PASS"); return; }
        catch (Fail f) { msg = f.Message; }
        catch (Exception e)
        {
            var frame = new StackTrace(e, true).GetFrames().FirstOrDefault(f => f.GetFileName() is string fn && !fn.Contains("__checker__"));
            var where = frame == null ? "" : $" at {Path.GetFileName(frame.GetFileName())}:{frame.GetFileLineNumber()}";
            msg = $"{e.GetType().Name}: {e.Message}{where}";
        }
        Console.WriteLine($"TEST {name} FAIL {msg.Replace('\n', ' ').Replace('\r', ' ')}");
    }

    static int Main()
    {
        asm = Assembly.Load("Warehouse.Core");

        Case("receive_and_ship", () =>
        {
            var m = Desk();
            Eq(m.OnHand("BOLT-M8"), 0, "OnHand of a SKU never received");
            m.Receive("BOLT-M8", 40);
            m.Receive("BOLT-M8", 2);
            m.Ship("BOLT-M8", 12);
            Eq(m.OnHand("BOLT-M8"), 30, "OnHand after receiving 42 and shipping 12");
            Eq(m.GetProduct("NUT-M8").UnitPrice, 0.12m, "GetProduct(NUT-M8).UnitPrice");
        });
        Case("stock_errors", () =>
        {
            var m = Desk();
            m.Receive("NUT-M8", 5);
            Throws<InvalidOperationException>(() => m.Ship("NUT-M8", 6), "Ship(6) with 5 on hand");
            Eq(m.OnHand("NUT-M8"), 5, "OnHand after a failed Ship");
            Throws<ArgumentException>(() => m.Receive("NOPE", 1), "Receive of an unknown SKU");
            Throws<ArgumentOutOfRangeException>(() => m.Receive("NUT-M8", 0), "Receive(0)");
            Throws<ArgumentOutOfRangeException>(() => m.Ship("NUT-M8", -1), "Ship(-1)");
            Throws<KeyNotFoundException>(() => m.GetProduct("NOPE"), "GetProduct of an unknown SKU");
            Throws<ArgumentException>(() => m.AddProduct(New("Product", "NUT-M8", "dup", 1m)), "AddProduct with a duplicate SKU");
            Throws<ArgumentOutOfRangeException>(() => m.AddProduct(New("Product", "X-1", "neg", -1m)), "AddProduct with a negative price");
        });
        Case("bulk_pricing", () =>
        {
            var m = Desk();
            Eq(m.LineTotal("BOLT-M8", 9), 3.15m, "LineTotal(BOLT-M8, 9)");
            Eq(m.LineTotal("BOLT-M8", 10), 3.33m, "LineTotal(BOLT-M8, 10)");
            Eq(m.LineTotal("BOLT-M8", 50), 15.75m, "LineTotal(BOLT-M8, 50)");
            Eq(m.LineTotal("BOLT-M8", 0), 0m, "LineTotal(BOLT-M8, 0)");
            Eq(m.LineTotal("DRILL-10", 3), 23.70m, "LineTotal(DRILL-10, 3)");
        });
        Case("order_filled", () =>
        {
            var m = Desk();
            m.Receive("BOLT-M8", 400);
            m.Receive("DRILL-10", 3);
            var r = m.PlaceOrder(Ord("SO-1", ("BOLT-M8", 120), ("DRILL-10", 3)));
            Eq(r.Status.ToString(), "Filled", "status");
            var lines = Items(r.Lines);
            ExpectLine(lines[0], "BOLT-M8", 120, 120, 0, 37.80m);
            ExpectLine(lines[1], "DRILL-10", 3, 3, 0, 23.70m);
            Eq(r.Total, 61.50m, "order total");
            Eq(m.OnHand("BOLT-M8"), 280, "BOLT-M8 on hand after the order");
            Eq(m.OnHand("DRILL-10"), 0, "DRILL-10 on hand after the order");
        });
        Case("order_partial", () =>
        {
            var m = Desk();
            m.Receive("BOLT-M8", 400);
            m.Receive("NUT-M8", 100);
            m.Receive("DRILL-10", 3);
            var r = m.PlaceOrder(Ord("SO-1001", ("BOLT-M8", 120), ("NUT-M8", 120), ("DRILL-10", 5)));
            Eq(r.Status.ToString(), "PartiallyFilled", "status");
            var lines = Items(r.Lines);
            Eq(lines.Count, 3, "number of result lines");
            ExpectLine(lines[0], "BOLT-M8", 120, 120, 0, 37.80m);
            ExpectLine(lines[1], "NUT-M8", 120, 100, 20, 10.80m);
            ExpectLine(lines[2], "DRILL-10", 5, 3, 2, 23.70m);
            Eq(r.Total, 72.30m, "order total");
            Eq($"{m.OnHand("BOLT-M8")}/{m.OnHand("NUT-M8")}/{m.OnHand("DRILL-10")}", "280/0/0", "stock after the order (BOLT/NUT/DRILL)");
        });
        Case("order_backordered", () =>
        {
            var m = Desk();
            var r = m.PlaceOrder(Ord("SO-2", ("DRILL-10", 2), ("NUT-M8", 7)));
            Eq(r.Status.ToString(), "Backordered", "status when nothing is in stock");
            var lines = Items(r.Lines);
            ExpectLine(lines[0], "DRILL-10", 2, 0, 2, 0m);
            ExpectLine(lines[1], "NUT-M8", 7, 0, 7, 0m);
            Eq(r.Total, 0m, "order total");
            Throws<ArgumentException>(() => Enum.Parse(Ty("OrderStatus"), "Rejected"), "OrderStatus.Rejected should be gone");
        });
        Case("order_same_sku_lines", () =>
        {
            var m = Desk();
            m.Receive("NUT-M8", 100);
            var r = m.PlaceOrder(Ord("SO-3", ("NUT-M8", 60), ("NUT-M8", 60)));
            Eq(r.Status.ToString(), "PartiallyFilled", "status");
            var lines = Items(r.Lines);
            ExpectLine(lines[0], "NUT-M8", 60, 60, 0, 6.48m);
            ExpectLine(lines[1], "NUT-M8", 60, 40, 20, 4.56m);
            Eq(m.OnHand("NUT-M8"), 0, "NUT-M8 on hand");
        });
        Case("order_validation", () =>
        {
            var m = Desk();
            m.Receive("BOLT-M8", 50);
            Throws<ArgumentException>(() => m.PlaceOrder(Ord("SO-4", ("BOLT-M8", 5), ("NOPE", 1))), "order with an unknown SKU");
            Throws<ArgumentException>(() => m.PlaceOrder(Ord("SO-5", ("BOLT-M8", 5), ("NUT-M8", 0))), "order with quantity 0");
            Throws<ArgumentException>(() => m.PlaceOrder(Ord("SO-6")), "order with no lines");
            Eq(m.OnHand("BOLT-M8"), 50, "BOLT-M8 on hand after rejected orders");
            Eq(Items(m.OrderHistory).Count, 0, "OrderHistory after only invalid orders");
        });
        Case("order_history", () =>
        {
            var m = Desk();
            m.Receive("BOLT-M8", 10);
            m.PlaceOrder(Ord("SO-7", ("BOLT-M8", 4)));
            m.PlaceOrder(Ord("SO-8", ("BOLT-M8", 8)));
            List<dynamic> h = Items(m.OrderHistory);
            Eq(string.Join(",", h.Select(x => (string)x.OrderId + ":" + x.Status.ToString())), "SO-7:Filled,SO-8:PartiallyFilled", "OrderHistory");
        });
        Case("reports", () =>
        {
            dynamic m = New("WarehouseManager");
            m.AddProduct(New("Product", "C3", "c", 3.00m));
            m.AddProduct(New("Product", "A1", "a", 2.00m));
            m.AddProduct(New("Product", "B2", "b", 1.00m));
            m.AddProduct(New("Product", "D4", "d", 0.10m));
            m.Receive("A1", 5);
            m.Receive("C3", 5);
            m.Receive("D4", 50);
            Eq(string.Join(",", (IEnumerable<string>)m.LowStock(10)), "B2,A1,C3", "LowStock(10)");
            Eq(string.Join(",", (IEnumerable<string>)m.LowStock(0)), "", "LowStock(0)");
            Eq(m.StockValuation(), 30.00m, "StockValuation()");
        });
        Case("public_types", () =>
        {
            var repo = Ty("IStockRepository");
            Check(repo.IsInterface, "IStockRepository must be an interface");
            foreach (var n in new[] { "OnHand", "Adjust" }) Check(repo.GetMethod(n) != null, $"IStockRepository has no {n} method");
            Check(repo.GetProperty("Skus") != null, "IStockRepository has no Skus property");
            foreach (var n in new[] { "InMemoryStockRepository", "Catalog", "PricingService", "OrderProcessor", "StockReport" })
                Check(Ty(n).IsClass && !Ty(n).IsAbstract, $"{n} must be a concrete class");
            Check(repo.IsAssignableFrom(Ty("InMemoryStockRepository")), "InMemoryStockRepository does not implement IStockRepository");
            Check(Ty("OrderProcessor").GetConstructor(new[] { Ty("Catalog"), repo, Ty("PricingService") }) != null,
                "OrderProcessor needs a public constructor (Catalog, IStockRepository, PricingService)");
            Check(Ty("StockReport").GetConstructor(new[] { Ty("Catalog"), repo }) != null,
                "StockReport needs a public constructor (Catalog, IStockRepository)");
            Check(Ty("WarehouseManager").GetConstructor(new[] { Ty("Catalog"), repo }) != null,
                "WarehouseManager needs a public constructor (Catalog, IStockRepository)");
        });
        Case("repository_contract", () =>
        {
            object r = New("InMemoryStockRepository");
            Eq(RepoOnHand(r, "X"), 0, "OnHand of an unknown SKU");
            RepoAdjust(r, "X", 7);
            RepoAdjust(r, "X", -2);
            Eq(RepoOnHand(r, "X"), 5, "OnHand after +7 -2");
            Throws<InvalidOperationException>(() => RepoAdjust(r, "X", -6), "Adjust below zero");
            Eq(RepoOnHand(r, "X"), 5, "OnHand after a refused Adjust");
            RepoAdjust(r, "Y", 1);
            var skus = ((IEnumerable<string>)Ty("IStockRepository").GetProperty("Skus").GetValue(r)).OrderBy(s => s, StringComparer.Ordinal);
            Eq(string.Join(",", skus), "X,Y", "Skus");
        });
        Case("components_standalone", () =>
        {
            dynamic catalog = New("Catalog");
            catalog.Add(New("Product", "P-1", "pump", 12.50m));
            catalog.Add(New("Product", "P-2", "hose", 2.00m));
            Check(catalog.Contains("P-1") && !catalog.Contains("P-9"), "Catalog.Contains");
            Eq(Items(catalog.All).Count, 2, "Catalog.All count");
            var proxy = DispatchProxy.Create(Ty("IStockRepository"), typeof(CountingRepo));
            var counting = (CountingRepo)proxy;
            counting.Inner = New("InMemoryStockRepository");
            RepoAdjust(proxy, "P-1", 4);
            counting.Calls.Clear();
            dynamic pricing = New("PricingService", catalog);
            Eq(pricing.LineTotal("P-2", 10), 19.00m, "PricingService.LineTotal(P-2, 10)");
            dynamic proc = New("OrderProcessor", catalog, proxy, pricing);
            var r = proc.Place(Ord("PO-1", ("P-1", 6)));
            ExpectLine(Items(r.Lines)[0], "P-1", 6, 4, 2, 50.00m);
            Eq(RepoOnHand(proxy, "P-1"), 0, "stock in the repository given to OrderProcessor");
            Check(counting.Calls.Contains("Adjust"), "OrderProcessor did not move stock through the IStockRepository it was given");
            Eq(Items(proc.History).Count, 1, "OrderProcessor.History count");
            dynamic report = New("StockReport", catalog, proxy);
            Eq(string.Join(",", (IEnumerable<string>)report.LowStock(1)), "P-1,P-2", "StockReport.LowStock(1)");
            Eq(report.Valuation(), 0m, "StockReport.Valuation()");
        });
        Case("facade_wiring", () =>
        {
            dynamic catalog = New("Catalog");
            object repo = New("InMemoryStockRepository");
            dynamic m = New("WarehouseManager", catalog, repo);
            Check(ReferenceEquals(m.Catalog, catalog), "WarehouseManager.Catalog is not the catalog it was given");
            Check(ReferenceEquals(m.Stock, repo), "WarehouseManager.Stock is not the repository it was given");
            Check(Ty("PricingService").IsInstanceOfType(m.Pricing), "WarehouseManager.Pricing is not a PricingService");
            Check(Ty("OrderProcessor").IsInstanceOfType(m.Orders), "WarehouseManager.Orders is not an OrderProcessor");
            Check(Ty("StockReport").IsInstanceOfType(m.Reports), "WarehouseManager.Reports is not a StockReport");
            m.AddProduct(New("Product", "K-1", "kit", 1.00m));
            Check(catalog.Contains("K-1"), "AddProduct did not go to the given Catalog");
            m.Receive("K-1", 3);
            Eq(RepoOnHand(repo, "K-1"), 3, "repository stock after WarehouseManager.Receive");
            RepoAdjust(repo, "K-1", 2);
            Eq(m.OnHand("K-1"), 5, "WarehouseManager.OnHand after adjusting the repository directly");
            m.PlaceOrder(Ord("SO-9", ("K-1", 1)));
            Eq(Items(m.Orders.History).Count, 1, "Orders.History after WarehouseManager.PlaceOrder");
            foreach (var f in Ty("WarehouseManager").GetFields(BindingFlags.Instance | BindingFlags.NonPublic | BindingFlags.Public | BindingFlags.DeclaredOnly))
                Check(f.FieldType == typeof(string) || !typeof(IEnumerable).IsAssignableFrom(f.FieldType),
                    $"WarehouseManager still keeps state of its own (field of type {f.FieldType.Name})");
        });
        return 0;
    }
}
