using System.Diagnostics;
using System.Text.RegularExpressions;
using System.Xml.Linq;
using Parcel.Core;

static class T
{
    sealed class Fail : Exception { public Fail(string m) : base(m) { } }

    static void Check(bool cond, string msg) { if (!cond) throw new Fail(msg); }

    static void Eq<TV>(TV got, TV want, string what)
    {
        if (!EqualityComparer<TV>.Default.Equals(got, want)) throw new Fail($"{what}: expected {want}, got {got}");
    }

    static void Throws<TE>(Action a, string what) where TE : Exception
    {
        try { a(); }
        catch (TE) { return; }
        catch (Exception e) { throw new Fail($"{what}: expected {typeof(TE).Name}, got {e.GetType().Name}"); }
        throw new Fail($"{what}: expected {typeof(TE).Name}, nothing was thrown");
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

    static (int Code, string Out, string Err) Cli(params string[] args)
    {
        var bin = Path.Combine("src", "Parcel.Cli", "bin");
        var dll = Directory.Exists(bin)
            ? Directory.GetFiles(bin, "Parcel.Cli.dll", SearchOption.AllDirectories).OrderBy(p => p.Contains("net8.0") ? 0 : 1).FirstOrDefault()
            : null;
        if (dll == null) throw new Fail("Parcel.Cli.dll was not built under src/Parcel.Cli/bin");
        var psi = new ProcessStartInfo("dotnet") { RedirectStandardOutput = true, RedirectStandardError = true, UseShellExecute = false };
        psi.ArgumentList.Add(dll);
        foreach (var a in args) psi.ArgumentList.Add(a);
        using var p = Process.Start(psi)!;
        var outTask = p.StandardOutput.ReadToEndAsync();
        var errTask = p.StandardError.ReadToEndAsync();
        if (!p.WaitForExit(30000)) { p.Kill(true); throw new Fail($"parcel {string.Join(' ', args)} did not exit within 30s"); }
        return (p.ExitCode, outTask.Result, errTask.Result);
    }

    static int Main()
    {
        Case("billable_weight_rounds_up", () =>
        {
            foreach (var (w, want) in new[] { (2.0m, 2.0m), (2.01m, 2.5m), (2.3m, 2.5m), (2.6m, 3.0m), (0.1m, 0.5m), (0.5m, 0.5m), (7.74m, 8.0m), (29.9m, 30.0m) })
                Eq(RateCalculator.BillableWeight(w), want, $"billable weight of {w} kg");
        });
        Case("weight_limits", () =>
        {
            foreach (var w in new[] { 0m, -1m, 30.01m })
                Throws<ArgumentOutOfRangeException>(() => RateCalculator.Quote(w, Zone.A), $"Quote({w} kg)");
            Eq(RateCalculator.Quote(30m, Zone.A).Price, 39.90m, "price of 30 kg in zone A");
        });
        Case("zone_prices", () =>
        {
            Eq(RateCalculator.Quote(2.3m, Zone.B).Price, 11.75m, "2.3 kg zone B");
            Eq(RateCalculator.Quote(1.0m, Zone.C).Price, 12.40m, "1.0 kg zone C");
            Eq(RateCalculator.Quote(10m, Zone.D).Price, 82.00m, "10 kg zone D");
            Eq(RateCalculator.Quote(4.2m, Zone.A).Price, 9.30m, "4.2 kg zone A");
            Eq(RateCalculator.Quote(4.2m, Zone.A).BillableKg, 4.5m, "billable kg of 4.2 kg");
        });
        Case("minimum_charge", () =>
        {
            Eq(RateCalculator.Quote(0.5m, Zone.A).Price, 5.00m, "0.5 kg zone A");
            Eq(RateCalculator.Quote(0.2m, Zone.A).Price, 5.00m, "0.2 kg zone A");
            Eq(RateCalculator.Quote(1.0m, Zone.A).Price, 5.10m, "1.0 kg zone A");
        });
        Case("zone_parsing", () =>
        {
            Eq(Zones.Parse("b"), Zone.B, "Parse(\"b\")");
            Eq(Zones.Parse(" C "), Zone.C, "Parse(\" C \")");
            Eq(Zones.Parse("D"), Zone.D, "Parse(\"D\")");
            Throws<ArgumentException>(() => Zones.Parse("E"), "Parse(\"E\")");
            Throws<ArgumentException>(() => Zones.Parse(""), "Parse(\"\")");
        });
        Case("label_with_company", () =>
        {
            var got = LabelPrinter.Format(new Recipient(" Ada Lovelace ", " Analytical Engines Ltd", "12 St James's Square ", " SW1Y 4JH London", "gb"));
            Eq(got, "Ada Lovelace\nAnalytical Engines Ltd\n12 St James's Square\nSW1Y 4JH London\nGB", "label");
        });
        Case("label_without_company", () =>
        {
            const string want = "Grace Hopper\n1 Navy Way\n20001 Washington\nUS";
            Eq(LabelPrinter.Format(new Recipient("Grace Hopper", null, "1 Navy Way", "20001 Washington", "us")), want, "label with null company");
            Eq(LabelPrinter.Format(new Recipient("Grace Hopper", "   ", "1 Navy Way", "20001 Washington", "US")), want, "label with blank company");
        });
        Case("cli_quote", () =>
        {
            var r = Cli("quote", "2.3", "b");
            Eq(r.Code, 0, "exit code of `parcel quote 2.3 b`");
            Eq(r.Out.TrimEnd(), "Zone B, 2.5 kg billable: 11.75 EUR", "stdout of `parcel quote 2.3 b`");
            r = Cli("quote", "0.3", "A");
            Eq(r.Out.TrimEnd(), "Zone A, 0.5 kg billable: 5.00 EUR", "stdout of `parcel quote 0.3 A`");
        });
        Case("cli_errors", () =>
        {
            var r = Cli();
            Eq(r.Code, 2, "exit code with no arguments");
            Check(r.Err.Contains("usage"), $"no arguments: expected a usage line on stderr, got '{r.Err.Trim()}'");
            r = Cli("quote", "2", "x");
            Eq(r.Code, 1, "exit code of `parcel quote 2 x`");
            Check(r.Err.StartsWith("error:"), $"`parcel quote 2 x`: expected stderr to start with 'error:', got '{r.Err.Trim()}'");
            r = Cli("quote", "31", "a");
            Eq(r.Code, 1, "exit code of `parcel quote 31 a`");
            Eq(r.Out, "", "stdout of `parcel quote 31 a`");
        });
        Case("strict_build_settings", () =>
        {
            var props = XDocument.Load("Directory.Build.props");
            string Prop(string n) => props.Descendants(n).LastOrDefault()?.Value.Trim() ?? "";
            Eq(Prop("TreatWarningsAsErrors").ToLowerInvariant(), "true", "TreatWarningsAsErrors in Directory.Build.props");
            Eq(Prop("Nullable").ToLowerInvariant(), "enable", "Nullable in Directory.Build.props");
            foreach (var proj in Directory.GetFiles("src", "*.csproj", SearchOption.AllDirectories))
            {
                var x = XDocument.Load(proj);
                var name = Path.GetFileName(proj);
                foreach (var el in x.Descendants())
                {
                    var n = el.Name.LocalName;
                    var v = el.Value.Trim().ToLowerInvariant();
                    Check(!(n == "TreatWarningsAsErrors" && v != "true"), $"{name} turns TreatWarningsAsErrors off");
                    Check(!(n == "Nullable" && v != "enable"), $"{name} sets Nullable to '{el.Value.Trim()}'");
                    Check(!((n == "NoWarn" || n == "WarningsNotAsErrors") && Regex.IsMatch(v, @"cs86|nullable")), $"{name} silences nullable warnings ({n})");
                }
            }
            foreach (var cs in Directory.GetFiles("src", "*.cs", SearchOption.AllDirectories).Where(p => !p.Contains($"{Path.DirectorySeparatorChar}obj{Path.DirectorySeparatorChar}")))
            {
                var text = File.ReadAllText(cs);
                Check(!Regex.IsMatch(text, @"#\s*nullable\s+disable|#\s*pragma\s+warning\s+disable"), $"{Path.GetFileName(cs)} disables warnings or nullable checks");
            }
        });
        return 0;
    }
}
