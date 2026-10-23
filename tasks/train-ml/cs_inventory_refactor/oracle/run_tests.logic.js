const SPEC = {
 "env": {
  "DOTNET_CLI_HOME": "$WORK/__checker__/home",
  "DOTNET_CLI_TELEMETRY_OPTOUT": "1",
  "DOTNET_CLI_USE_MSBUILD_SERVER": "0",
  "DOTNET_GENERATE_ASPNET_CERTIFICATE": "false",
  "DOTNET_NOLOGO": "1",
  "DOTNET_SKIP_FIRST_TIME_EXPERIENCE": "1",
  "DOTNET_SKIP_WORKLOAD_INTEGRITY_CHECK": "1",
  "HOME": "$WORK/__checker__/home",
  "MSBUILDDISABLENODEREUSE": "1",
  "NUGET_PACKAGES": "$WORK/__checker__/home/.nuget/packages"
 },
 "files": {
  "__checker__/Checker.csproj": "<Project Sdk=\"Microsoft.NET.Sdk\">\n  <PropertyGroup>\n    <OutputType>Exe</OutputType>\n    <TargetFramework>net8.0</TargetFramework>\n    <ImplicitUsings>enable</ImplicitUsings>\n    <Nullable>disable</Nullable>\n    <UseAppHost>false</UseAppHost>\n    <TreatWarningsAsErrors>false</TreatWarningsAsErrors>\n  </PropertyGroup>\n  <ItemGroup>\n    <ProjectReference Include=\"../src/Warehouse.Core/Warehouse.Core.csproj\" />\n  </ItemGroup>\n</Project>\n",
  "__checker__/Directory.Build.props": "<Project>\n  <!-- stops MSBuild from importing the workspace's Directory.Build.props into the hidden test project -->\n</Project>\n",
  "__checker__/Tests.cs": "using System.Collections;\nusing System.Diagnostics;\nusing System.Reflection;\n\npublic class CountingRepo : DispatchProxy\n{\n    public object Inner;\n    public List<string> Calls = new();\n\n    protected override object Invoke(MethodInfo method, object[] args)\n    {\n        Calls.Add(method.Name);\n        try { return method.Invoke(Inner, args); }\n        catch (TargetInvocationException e) { throw e.InnerException; }\n    }\n}\n\nstatic class T\n{\n    sealed class Fail : Exception { public Fail(string m) : base(m) { } }\n\n    static Assembly asm;\n\n    static void Check(bool cond, string msg) { if (!cond) throw new Fail(msg); }\n\n    static void Eq(object got, object want, string what)\n    {\n        if (!Equals(got, want)) throw new Fail($\"{what}: expected {want}, got {got ?? \"null\"}\");\n    }\n\n    static void Throws<TE>(Action a, string what) where TE : Exception\n    {\n        try { a(); }\n        catch (TE) { return; }\n        catch (Fail) { throw; }\n        catch (Exception e) { throw new Fail($\"{what}: expected {typeof(TE).Name}, got {e.GetType().Name}: {e.Message}\"); }\n        throw new Fail($\"{what}: expected {typeof(TE).Name}, nothing was thrown\");\n    }\n\n    static Type Ty(string name)\n    {\n        var t = asm.GetType(\"Warehouse.Core.\" + name);\n        if (t == null || !t.IsPublic) throw new Fail($\"public type Warehouse.Core.{name} not found\");\n        return t;\n    }\n\n    static dynamic New(string name, params object[] args)\n    {\n        var t = Ty(name);\n        try { return Activator.CreateInstance(t, args); }\n        catch (MissingMethodException)\n        {\n            throw new Fail($\"{name} has no public constructor taking ({string.Join(\", \", args.Select(a => a?.GetType().Name ?? \"null\"))})\");\n        }\n        catch (TargetInvocationException e) { throw e.InnerException; }\n    }\n\n    static dynamic Ord(string id, params (string Sku, int Qty)[] lines)\n    {\n        var lt = Ty(\"OrderLine\");\n        var arr = Array.CreateInstance(lt, lines.Length);\n        for (var i = 0; i < lines.Length; i++) arr.SetValue(Activator.CreateInstance(lt, lines[i].Sku, lines[i].Qty), i);\n        return Activator.CreateInstance(Ty(\"Order\"), id, \"Fixit GmbH\", arr);\n    }\n\n    static List<dynamic> Items(object seq)\n    {\n        var l = new List<dynamic>();\n        foreach (var x in (IEnumerable)seq) l.Add(x);\n        return l;\n    }\n\n    static int RepoOnHand(object repo, string sku) => (int)Call(repo, \"OnHand\", sku);\n    static void RepoAdjust(object repo, string sku, int delta) => Call(repo, \"Adjust\", sku, delta);\n\n    static object Call(object repo, string method, params object[] args)\n    {\n        var m = Ty(\"IStockRepository\").GetMethod(method) ?? throw new Fail($\"IStockRepository has no method {method}\");\n        try { return m.Invoke(repo, args); }\n        catch (TargetInvocationException e) { throw e.InnerException; }\n    }\n\n    static dynamic Desk()\n    {\n        dynamic m = New(\"WarehouseManager\");\n        m.AddProduct(New(\"Product\", \"BOLT-M8\", \"M8 hex bolt\", 0.35m));\n        m.AddProduct(New(\"Product\", \"NUT-M8\", \"M8 nut\", 0.12m));\n        m.AddProduct(New(\"Product\", \"DRILL-10\", \"10 mm drill bit\", 7.90m));\n        return m;\n    }\n\n    static void ExpectLine(dynamic line, string sku, int requested, int shipped, int backordered, decimal total)\n    {\n        string got = $\"{line.Sku} req {line.Requested} shipped {line.Shipped} backordered {line.Backordered} total {(decimal)line.Total:0.00}\";\n        string want = $\"{sku} req {requested} shipped {shipped} backordered {backordered} total {total:0.00}\";\n        Eq(got, want, \"order line\");\n    }\n\n    static void Case(string name, Action fn)\n    {\n        string msg;\n        try { fn(); Console.WriteLine($\"TEST {name} PASS\"); return; }\n        catch (Fail f) { msg = f.Message; }\n        catch (Exception e)\n        {\n            var frame = new StackTrace(e, true).GetFrames().FirstOrDefault(f => f.GetFileName() is string fn && !fn.Contains(\"__checker__\"));\n            var where = frame == null ? \"\" : $\" at {Path.GetFileName(frame.GetFileName())}:{frame.GetFileLineNumber()}\";\n            msg = $\"{e.GetType().Name}: {e.Message}{where}\";\n        }\n        Console.WriteLine($\"TEST {name} FAIL {msg.Replace('\\n', ' ').Replace('\\r', ' ')}\");\n    }\n\n    static int Main()\n    {\n        asm = Assembly.Load(\"Warehouse.Core\");\n\n        Case(\"receive_and_ship\", () =>\n        {\n            var m = Desk();\n            Eq(m.OnHand(\"BOLT-M8\"), 0, \"OnHand of a SKU never received\");\n            m.Receive(\"BOLT-M8\", 40);\n            m.Receive(\"BOLT-M8\", 2);\n            m.Ship(\"BOLT-M8\", 12);\n            Eq(m.OnHand(\"BOLT-M8\"), 30, \"OnHand after receiving 42 and shipping 12\");\n            Eq(m.GetProduct(\"NUT-M8\").UnitPrice, 0.12m, \"GetProduct(NUT-M8).UnitPrice\");\n        });\n        Case(\"stock_errors\", () =>\n        {\n            var m = Desk();\n            m.Receive(\"NUT-M8\", 5);\n            Throws<InvalidOperationException>(() => m.Ship(\"NUT-M8\", 6), \"Ship(6) with 5 on hand\");\n            Eq(m.OnHand(\"NUT-M8\"), 5, \"OnHand after a failed Ship\");\n            Throws<ArgumentException>(() => m.Receive(\"NOPE\", 1), \"Receive of an unknown SKU\");\n            Throws<ArgumentOutOfRangeException>(() => m.Receive(\"NUT-M8\", 0), \"Receive(0)\");\n            Throws<ArgumentOutOfRangeException>(() => m.Ship(\"NUT-M8\", -1), \"Ship(-1)\");\n            Throws<KeyNotFoundException>(() => m.GetProduct(\"NOPE\"), \"GetProduct of an unknown SKU\");\n            Throws<ArgumentException>(() => m.AddProduct(New(\"Product\", \"NUT-M8\", \"dup\", 1m)), \"AddProduct with a duplicate SKU\");\n            Throws<ArgumentOutOfRangeException>(() => m.AddProduct(New(\"Product\", \"X-1\", \"neg\", -1m)), \"AddProduct with a negative price\");\n        });\n        Case(\"bulk_pricing\", () =>\n        {\n            var m = Desk();\n            Eq(m.LineTotal(\"BOLT-M8\", 9), 3.15m, \"LineTotal(BOLT-M8, 9)\");\n            Eq(m.LineTotal(\"BOLT-M8\", 10), 3.33m, \"LineTotal(BOLT-M8, 10)\");\n            Eq(m.LineTotal(\"BOLT-M8\", 50), 15.75m, \"LineTotal(BOLT-M8, 50)\");\n            Eq(m.LineTotal(\"BOLT-M8\", 0), 0m, \"LineTotal(BOLT-M8, 0)\");\n            Eq(m.LineTotal(\"DRILL-10\", 3), 23.70m, \"LineTotal(DRILL-10, 3)\");\n        });\n        Case(\"order_filled\", () =>\n        {\n            var m = Desk();\n            m.Receive(\"BOLT-M8\", 400);\n            m.Receive(\"DRILL-10\", 3);\n            var r = m.PlaceOrder(Ord(\"SO-1\", (\"BOLT-M8\", 120), (\"DRILL-10\", 3)));\n            Eq(r.Status.ToString(), \"Filled\", \"status\");\n            var lines = Items(r.Lines);\n            ExpectLine(lines[0], \"BOLT-M8\", 120, 120, 0, 37.80m);\n            ExpectLine(lines[1], \"DRILL-10\", 3, 3, 0, 23.70m);\n            Eq(r.Total, 61.50m, \"order total\");\n            Eq(m.OnHand(\"BOLT-M8\"), 280, \"BOLT-M8 on hand after the order\");\n            Eq(m.OnHand(\"DRILL-10\"), 0, \"DRILL-10 on hand after the order\");\n        });\n        Case(\"order_partial\", () =>\n        {\n            var m = Desk();\n            m.Receive(\"BOLT-M8\", 400);\n            m.Receive(\"NUT-M8\", 100);\n            m.Receive(\"DRILL-10\", 3);\n            var r = m.PlaceOrder(Ord(\"SO-1001\", (\"BOLT-M8\", 120), (\"NUT-M8\", 120), (\"DRILL-10\", 5)));\n            Eq(r.Status.ToString(), \"PartiallyFilled\", \"status\");\n            var lines = Items(r.Lines);\n            Eq(lines.Count, 3, \"number of result lines\");\n            ExpectLine(lines[0], \"BOLT-M8\", 120, 120, 0, 37.80m);\n            ExpectLine(lines[1], \"NUT-M8\", 120, 100, 20, 10.80m);\n            ExpectLine(lines[2], \"DRILL-10\", 5, 3, 2, 23.70m);\n            Eq(r.Total, 72.30m, \"order total\");\n            Eq($\"{m.OnHand(\"BOLT-M8\")}/{m.OnHand(\"NUT-M8\")}/{m.OnHand(\"DRILL-10\")}\", \"280/0/0\", \"stock after the order (BOLT/NUT/DRILL)\");\n        });\n        Case(\"order_backordered\", () =>\n        {\n            var m = Desk();\n            var r = m.PlaceOrder(Ord(\"SO-2\", (\"DRILL-10\", 2), (\"NUT-M8\", 7)));\n            Eq(r.Status.ToString(), \"Backordered\", \"status when nothing is in stock\");\n            var lines = Items(r.Lines);\n            ExpectLine(lines[0], \"DRILL-10\", 2, 0, 2, 0m);\n            ExpectLine(lines[1], \"NUT-M8\", 7, 0, 7, 0m);\n            Eq(r.Total, 0m, \"order total\");\n            Throws<ArgumentException>(() => Enum.Parse(Ty(\"OrderStatus\"), \"Rejected\"), \"OrderStatus.Rejected should be gone\");\n        });\n        Case(\"order_same_sku_lines\", () =>\n        {\n            var m = Desk();\n            m.Receive(\"NUT-M8\", 100);\n            var r = m.PlaceOrder(Ord(\"SO-3\", (\"NUT-M8\", 60), (\"NUT-M8\", 60)));\n            Eq(r.Status.ToString(), \"PartiallyFilled\", \"status\");\n            var lines = Items(r.Lines);\n            ExpectLine(lines[0], \"NUT-M8\", 60, 60, 0, 6.48m);\n            ExpectLine(lines[1], \"NUT-M8\", 60, 40, 20, 4.56m);\n            Eq(m.OnHand(\"NUT-M8\"), 0, \"NUT-M8 on hand\");\n        });\n        Case(\"order_validation\", () =>\n        {\n            var m = Desk();\n            m.Receive(\"BOLT-M8\", 50);\n            Throws<ArgumentException>(() => m.PlaceOrder(Ord(\"SO-4\", (\"BOLT-M8\", 5), (\"NOPE\", 1))), \"order with an unknown SKU\");\n            Throws<ArgumentException>(() => m.PlaceOrder(Ord(\"SO-5\", (\"BOLT-M8\", 5), (\"NUT-M8\", 0))), \"order with quantity 0\");\n            Throws<ArgumentException>(() => m.PlaceOrder(Ord(\"SO-6\")), \"order with no lines\");\n            Eq(m.OnHand(\"BOLT-M8\"), 50, \"BOLT-M8 on hand after rejected orders\");\n            Eq(Items(m.OrderHistory).Count, 0, \"OrderHistory after only invalid orders\");\n        });\n        Case(\"order_history\", () =>\n        {\n            var m = Desk();\n            m.Receive(\"BOLT-M8\", 10);\n            m.PlaceOrder(Ord(\"SO-7\", (\"BOLT-M8\", 4)));\n            m.PlaceOrder(Ord(\"SO-8\", (\"BOLT-M8\", 8)));\n            List<dynamic> h = Items(m.OrderHistory);\n            Eq(string.Join(\",\", h.Select(x => (string)x.OrderId + \":\" + x.Status.ToString())), \"SO-7:Filled,SO-8:PartiallyFilled\", \"OrderHistory\");\n        });\n        Case(\"reports\", () =>\n        {\n            dynamic m = New(\"WarehouseManager\");\n            m.AddProduct(New(\"Product\", \"C3\", \"c\", 3.00m));\n            m.AddProduct(New(\"Product\", \"A1\", \"a\", 2.00m));\n            m.AddProduct(New(\"Product\", \"B2\", \"b\", 1.00m));\n            m.AddProduct(New(\"Product\", \"D4\", \"d\", 0.10m));\n            m.Receive(\"A1\", 5);\n            m.Receive(\"C3\", 5);\n            m.Receive(\"D4\", 50);\n            Eq(string.Join(\",\", (IEnumerable<string>)m.LowStock(10)), \"B2,A1,C3\", \"LowStock(10)\");\n            Eq(string.Join(\",\", (IEnumerable<string>)m.LowStock(0)), \"\", \"LowStock(0)\");\n            Eq(m.StockValuation(), 30.00m, \"StockValuation()\");\n        });\n        Case(\"public_types\", () =>\n        {\n            var repo = Ty(\"IStockRepository\");\n            Check(repo.IsInterface, \"IStockRepository must be an interface\");\n            foreach (var n in new[] { \"OnHand\", \"Adjust\" }) Check(repo.GetMethod(n) != null, $\"IStockRepository has no {n} method\");\n            Check(repo.GetProperty(\"Skus\") != null, \"IStockRepository has no Skus property\");\n            foreach (var n in new[] { \"InMemoryStockRepository\", \"Catalog\", \"PricingService\", \"OrderProcessor\", \"StockReport\" })\n                Check(Ty(n).IsClass && !Ty(n).IsAbstract, $\"{n} must be a concrete class\");\n            Check(repo.IsAssignableFrom(Ty(\"InMemoryStockRepository\")), \"InMemoryStockRepository does not implement IStockRepository\");\n            Check(Ty(\"OrderProcessor\").GetConstructor(new[] { Ty(\"Catalog\"), repo, Ty(\"PricingService\") }) != null,\n                \"OrderProcessor needs a public constructor (Catalog, IStockRepository, PricingService)\");\n            Check(Ty(\"StockReport\").GetConstructor(new[] { Ty(\"Catalog\"), repo }) != null,\n                \"StockReport needs a public constructor (Catalog, IStockRepository)\");\n            Check(Ty(\"WarehouseManager\").GetConstructor(new[] { Ty(\"Catalog\"), repo }) != null,\n                \"WarehouseManager needs a public constructor (Catalog, IStockRepository)\");\n        });\n        Case(\"repository_contract\", () =>\n        {\n            object r = New(\"InMemoryStockRepository\");\n            Eq(RepoOnHand(r, \"X\"), 0, \"OnHand of an unknown SKU\");\n            RepoAdjust(r, \"X\", 7);\n            RepoAdjust(r, \"X\", -2);\n            Eq(RepoOnHand(r, \"X\"), 5, \"OnHand after +7 -2\");\n            Throws<InvalidOperationException>(() => RepoAdjust(r, \"X\", -6), \"Adjust below zero\");\n            Eq(RepoOnHand(r, \"X\"), 5, \"OnHand after a refused Adjust\");\n            RepoAdjust(r, \"Y\", 1);\n            var skus = ((IEnumerable<string>)Ty(\"IStockRepository\").GetProperty(\"Skus\").GetValue(r)).OrderBy(s => s, StringComparer.Ordinal);\n            Eq(string.Join(\",\", skus), \"X,Y\", \"Skus\");\n        });\n        Case(\"components_standalone\", () =>\n        {\n            dynamic catalog = New(\"Catalog\");\n            catalog.Add(New(\"Product\", \"P-1\", \"pump\", 12.50m));\n            catalog.Add(New(\"Product\", \"P-2\", \"hose\", 2.00m));\n            Check(catalog.Contains(\"P-1\") && !catalog.Contains(\"P-9\"), \"Catalog.Contains\");\n            Eq(Items(catalog.All).Count, 2, \"Catalog.All count\");\n            var proxy = DispatchProxy.Create(Ty(\"IStockRepository\"), typeof(CountingRepo));\n            var counting = (CountingRepo)proxy;\n            counting.Inner = New(\"InMemoryStockRepository\");\n            RepoAdjust(proxy, \"P-1\", 4);\n            counting.Calls.Clear();\n            dynamic pricing = New(\"PricingService\", catalog);\n            Eq(pricing.LineTotal(\"P-2\", 10), 19.00m, \"PricingService.LineTotal(P-2, 10)\");\n            dynamic proc = New(\"OrderProcessor\", catalog, proxy, pricing);\n            var r = proc.Place(Ord(\"PO-1\", (\"P-1\", 6)));\n            ExpectLine(Items(r.Lines)[0], \"P-1\", 6, 4, 2, 50.00m);\n            Eq(RepoOnHand(proxy, \"P-1\"), 0, \"stock in the repository given to OrderProcessor\");\n            Check(counting.Calls.Contains(\"Adjust\"), \"OrderProcessor did not move stock through the IStockRepository it was given\");\n            Eq(Items(proc.History).Count, 1, \"OrderProcessor.History count\");\n            dynamic report = New(\"StockReport\", catalog, proxy);\n            Eq(string.Join(\",\", (IEnumerable<string>)report.LowStock(1)), \"P-1,P-2\", \"StockReport.LowStock(1)\");\n            Eq(report.Valuation(), 0m, \"StockReport.Valuation()\");\n        });\n        Case(\"facade_wiring\", () =>\n        {\n            dynamic catalog = New(\"Catalog\");\n            object repo = New(\"InMemoryStockRepository\");\n            dynamic m = New(\"WarehouseManager\", catalog, repo);\n            Check(ReferenceEquals(m.Catalog, catalog), \"WarehouseManager.Catalog is not the catalog it was given\");\n            Check(ReferenceEquals(m.Stock, repo), \"WarehouseManager.Stock is not the repository it was given\");\n            Check(Ty(\"PricingService\").IsInstanceOfType(m.Pricing), \"WarehouseManager.Pricing is not a PricingService\");\n            Check(Ty(\"OrderProcessor\").IsInstanceOfType(m.Orders), \"WarehouseManager.Orders is not an OrderProcessor\");\n            Check(Ty(\"StockReport\").IsInstanceOfType(m.Reports), \"WarehouseManager.Reports is not a StockReport\");\n            m.AddProduct(New(\"Product\", \"K-1\", \"kit\", 1.00m));\n            Check(catalog.Contains(\"K-1\"), \"AddProduct did not go to the given Catalog\");\n            m.Receive(\"K-1\", 3);\n            Eq(RepoOnHand(repo, \"K-1\"), 3, \"repository stock after WarehouseManager.Receive\");\n            RepoAdjust(repo, \"K-1\", 2);\n            Eq(m.OnHand(\"K-1\"), 5, \"WarehouseManager.OnHand after adjusting the repository directly\");\n            m.PlaceOrder(Ord(\"SO-9\", (\"K-1\", 1)));\n            Eq(Items(m.Orders.History).Count, 1, \"Orders.History after WarehouseManager.PlaceOrder\");\n            foreach (var f in Ty(\"WarehouseManager\").GetFields(BindingFlags.Instance | BindingFlags.NonPublic | BindingFlags.Public | BindingFlags.DeclaredOnly))\n                Check(f.FieldType == typeof(string) || !typeof(IEnumerable).IsAssignableFrom(f.FieldType),\n                    $\"WarehouseManager still keeps state of its own (field of type {f.FieldType.Name})\");\n        });\n        return 0;\n    }\n}\n",
  "__checker__/home/.local/share/NuGet/Migrations/1": ""
 },
 "hidden_dir": "__checker__",
 "steps": [
  {
   "cmd": [
    "sh",
    "-c",
    "rm -rf src/*/bin src/*/obj"
   ],
   "label": "clean",
   "timeout_s": 30
  },
  {
   "cmd": [
    "dotnet",
    "build",
    "Warehouse.sln",
    "--nologo",
    "--disable-build-servers",
    "-nodeReuse:false",
    "-p:UseSharedCompilation=false",
    "-clp:NoSummary"
   ],
   "label": "dotnet build Warehouse.sln",
   "timeout_s": 180
  },
  {
   "cmd": [
    "dotnet",
    "build",
    "__checker__/Checker.csproj",
    "--nologo",
    "--disable-build-servers",
    "-nodeReuse:false",
    "-p:UseSharedCompilation=false",
    "-clp:NoSummary"
   ],
   "label": "build the hidden tests",
   "timeout_s": 180
  },
  {
   "cmd": [
    "dotnet",
    "__checker__/bin/Debug/net8.0/Checker.dll"
   ],
   "label": "tests",
   "tests": true,
   "timeout_s": 120
  }
 ],
 "tests": [
  "receive_and_ship",
  "stock_errors",
  "bulk_pricing",
  "order_filled",
  "order_partial",
  "order_backordered",
  "order_same_sku_lines",
  "order_validation",
  "order_history",
  "reports",
  "public_types",
  "repository_contract",
  "components_standalone",
  "facade_wiring"
 ]
};
// The checker every training task ships as `run_tests`: the agent may run it, and cannot read it.
//
// make_task.py prepends `const SPEC = {...};` (the task's hidden test files, its build and test
// steps, the test names it must see) and build.mjs (manic_miner's) turns the result into a Node
// single executable whose source is masked.
//
// It copies the working directory to a private temporary directory, writes the hidden tests into
// it, runs the steps there with the language's own tools, and prints ONE JSON line:
//
//   {"ok":true|false,"stage":"build"|"test"|"done","passed":N,"total":N,"failures":[...],"output":"..."}
//
// Test programs report one line per test on stdout:  TEST <name> PASS   or   TEST <name> FAIL <message>
// "ok" needs every step to exit 0 and every name in SPEC.tests to report PASS: a test that never
// reports (the program died, or exited early) is a failure. Output lines that point into the hidden
// test directory are dropped, so a build error never prints the tests' source.
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const cp = require("node:child_process");

const HIDDEN = SPEC.hidden_dir || "__checker__";
const MAX_OUT = 6000;

function copyTree(src, dst) {
	fs.mkdirSync(dst, { recursive: true });
	for (const e of fs.readdirSync(src, { withFileTypes: true })) {
		if (e.name === ".git" || e.name === "run_tests" || e.name === HIDDEN) continue;
		const s = path.join(src, e.name);
		const d = path.join(dst, e.name);
		if (e.isDirectory()) copyTree(s, d);
		else if (e.isFile()) fs.copyFileSync(s, d);
	}
}

// A failure message is the test's own words, but an error inside it (a stack, a module path) can name a
// hidden file: any token that contains the hidden directory's name is replaced.
function hide(text) {
	return String(text).replace(new RegExp(`\\S*${HIDDEN}\\S*`, "g"), "<checker>");
}

// A compiler names the file on one line and quotes its source on the next ones ("  12 | code", a "^~~~"
// marker line): after a line that names a hidden file, those quoted lines are dropped as well.
const QUOTED = /^\s*(\d+\s*)?\|/;
const MARKER = /^\s*\|?\s*[\^~\-+]+[\s\^~\-+]*$/;

function scrub(text) {
	const lines = [];
	let quoting = false;
	for (const l of String(text || "").split("\n")) {
		if (l.includes(HIDDEN)) {
			quoting = true;
			continue;
		}
		if (quoting && (QUOTED.test(l) || MARKER.test(l) || /^\s*$/.test(l))) continue;
		quoting = false;
		if (!/^TEST /.test(l)) lines.push(l);
	}
	const out = lines.join("\n").trim();
	return out.length > MAX_OUT ? `${out.slice(0, MAX_OUT)}\n[... output cut]` : out;
}

function finish(res, work) {
	try {
		fs.rmSync(work, { recursive: true, force: true });
	} catch {}
	process.stdout.write(`${JSON.stringify(res)}\n`);
	process.exit(0);
}

const work = fs.mkdtempSync(path.join(os.tmpdir(), "chk-"));
const res = { ok: false, stage: "build", passed: 0, total: SPEC.tests.length, failures: [], output: "" };
try {
	copyTree(process.cwd(), work);
	for (const [rel, body] of Object.entries(SPEC.files)) {
		const p = path.join(work, rel);
		fs.mkdirSync(path.dirname(p), { recursive: true });
		fs.writeFileSync(p, body);
	}
} catch (e) {
	res.output = `could not prepare the check: ${e.message}`;
	finish(res, work);
}

const env = {
	PATH: process.env.PATH || "/usr/local/bin:/usr/bin:/bin",
	HOME: work,
	TMPDIR: work,
	LANG: "C.UTF-8",
	...(SPEC.env || {}),
};
for (const [k, v] of Object.entries(env)) env[k] = String(v).replaceAll("$WORK", work);

const seen = new Map();
let output = "";
for (const step of SPEC.steps) {
	const cmd = step.cmd.map((a) => String(a).replaceAll("$WORK", work));
	const r = cp.spawnSync(cmd[0], cmd.slice(1), {
		cwd: work,
		env,
		encoding: "utf8",
		timeout: (step.timeout_s || 120) * 1000,
		maxBuffer: 64 * 1024 * 1024,
	});
	const text = `${r.stdout || ""}\n${r.stderr || ""}`;
	if (step.tests) {
		res.stage = "test";
		for (const m of String(r.stdout || "").matchAll(/^TEST (\S+) (PASS|FAIL)(?: (.*))?$/gm)) {
			if (!seen.has(m[1]) || m[2] === "FAIL") seen.set(m[1], m[2] === "PASS" ? null : hide(m[3] || "failed").slice(0, 300));
		}
	}
	output += `${scrub(text)}\n`;
	if (r.error && r.error.code === "ETIMEDOUT") {
		res.output = scrub(`${output}\nstep timed out after ${step.timeout_s || 120}s: ${step.label || step.cmd[0]}`);
		break;
	}
	if (r.error) {
		res.output = scrub(`${output}\ncould not run ${cmd[0]}: ${r.error.message}`);
		break;
	}
	if (r.status !== 0 && !step.tests) {
		res.output = scrub(`${output}\n${step.label || step.cmd[0]} failed (exit ${r.status})`);
		break;
	}
	if (step.tests && r.status !== 0 && seen.size === 0) {
		res.output = scrub(`${output}\nthe tests did not run (exit ${r.status})`);
		break;
	}
}
for (const name of SPEC.tests) {
	if (!seen.has(name)) res.failures.push({ test: name, message: "did not run" });
	else if (seen.get(name) !== null) res.failures.push({ test: name, message: seen.get(name) });
	else res.passed += 1;
}
if (res.stage === "test" && !res.output) {
	res.stage = "done";
	res.output = scrub(output);
}
res.ok = res.stage === "done" && res.passed === res.total;
finish(res, work);
