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
  "__checker__/Checker.csproj": "<Project Sdk=\"Microsoft.NET.Sdk\">\n  <PropertyGroup>\n    <OutputType>Exe</OutputType>\n    <TargetFramework>net8.0</TargetFramework>\n    <ImplicitUsings>enable</ImplicitUsings>\n    <Nullable>disable</Nullable>\n    <UseAppHost>false</UseAppHost>\n    <TreatWarningsAsErrors>false</TreatWarningsAsErrors>\n    <NoWarn>$(NoWarn);CS8632</NoWarn>\n  </PropertyGroup>\n  <ItemGroup>\n    <ProjectReference Include=\"../src/Parcel.Core/Parcel.Core.csproj\" />\n  </ItemGroup>\n</Project>\n",
  "__checker__/Directory.Build.props": "<Project>\n  <!-- stops MSBuild from importing the workspace's Directory.Build.props into the hidden test project -->\n</Project>\n",
  "__checker__/Tests.cs": "using System.Diagnostics;\nusing System.Text.RegularExpressions;\nusing System.Xml.Linq;\nusing Parcel.Core;\n\nstatic class T\n{\n    sealed class Fail : Exception { public Fail(string m) : base(m) { } }\n\n    static void Check(bool cond, string msg) { if (!cond) throw new Fail(msg); }\n\n    static void Eq<TV>(TV got, TV want, string what)\n    {\n        if (!EqualityComparer<TV>.Default.Equals(got, want)) throw new Fail($\"{what}: expected {want}, got {got}\");\n    }\n\n    static void Throws<TE>(Action a, string what) where TE : Exception\n    {\n        try { a(); }\n        catch (TE) { return; }\n        catch (Exception e) { throw new Fail($\"{what}: expected {typeof(TE).Name}, got {e.GetType().Name}\"); }\n        throw new Fail($\"{what}: expected {typeof(TE).Name}, nothing was thrown\");\n    }\n\n    static void Case(string name, Action fn)\n    {\n        string msg;\n        try { fn(); Console.WriteLine($\"TEST {name} PASS\"); return; }\n        catch (Fail f) { msg = f.Message; }\n        catch (Exception e)\n        {\n            var frame = new StackTrace(e, true).GetFrames().FirstOrDefault(f => f.GetFileName() is string fn && !fn.Contains(\"__checker__\"));\n            var where = frame == null ? \"\" : $\" at {Path.GetFileName(frame.GetFileName())}:{frame.GetFileLineNumber()}\";\n            msg = $\"{e.GetType().Name}: {e.Message}{where}\";\n        }\n        Console.WriteLine($\"TEST {name} FAIL {msg.Replace('\\n', ' ').Replace('\\r', ' ')}\");\n    }\n\n    static (int Code, string Out, string Err) Cli(params string[] args)\n    {\n        var bin = Path.Combine(\"src\", \"Parcel.Cli\", \"bin\");\n        var dll = Directory.Exists(bin)\n            ? Directory.GetFiles(bin, \"Parcel.Cli.dll\", SearchOption.AllDirectories).OrderBy(p => p.Contains(\"net8.0\") ? 0 : 1).FirstOrDefault()\n            : null;\n        if (dll == null) throw new Fail(\"Parcel.Cli.dll was not built under src/Parcel.Cli/bin\");\n        var psi = new ProcessStartInfo(\"dotnet\") { RedirectStandardOutput = true, RedirectStandardError = true, UseShellExecute = false };\n        psi.ArgumentList.Add(dll);\n        foreach (var a in args) psi.ArgumentList.Add(a);\n        using var p = Process.Start(psi)!;\n        var outTask = p.StandardOutput.ReadToEndAsync();\n        var errTask = p.StandardError.ReadToEndAsync();\n        if (!p.WaitForExit(30000)) { p.Kill(true); throw new Fail($\"parcel {string.Join(' ', args)} did not exit within 30s\"); }\n        return (p.ExitCode, outTask.Result, errTask.Result);\n    }\n\n    static int Main()\n    {\n        Case(\"billable_weight_rounds_up\", () =>\n        {\n            foreach (var (w, want) in new[] { (2.0m, 2.0m), (2.01m, 2.5m), (2.3m, 2.5m), (2.6m, 3.0m), (0.1m, 0.5m), (0.5m, 0.5m), (7.74m, 8.0m), (29.9m, 30.0m) })\n                Eq(RateCalculator.BillableWeight(w), want, $\"billable weight of {w} kg\");\n        });\n        Case(\"weight_limits\", () =>\n        {\n            foreach (var w in new[] { 0m, -1m, 30.01m })\n                Throws<ArgumentOutOfRangeException>(() => RateCalculator.Quote(w, Zone.A), $\"Quote({w} kg)\");\n            Eq(RateCalculator.Quote(30m, Zone.A).Price, 39.90m, \"price of 30 kg in zone A\");\n        });\n        Case(\"zone_prices\", () =>\n        {\n            Eq(RateCalculator.Quote(2.3m, Zone.B).Price, 11.75m, \"2.3 kg zone B\");\n            Eq(RateCalculator.Quote(1.0m, Zone.C).Price, 12.40m, \"1.0 kg zone C\");\n            Eq(RateCalculator.Quote(10m, Zone.D).Price, 82.00m, \"10 kg zone D\");\n            Eq(RateCalculator.Quote(4.2m, Zone.A).Price, 9.30m, \"4.2 kg zone A\");\n            Eq(RateCalculator.Quote(4.2m, Zone.A).BillableKg, 4.5m, \"billable kg of 4.2 kg\");\n        });\n        Case(\"minimum_charge\", () =>\n        {\n            Eq(RateCalculator.Quote(0.5m, Zone.A).Price, 5.00m, \"0.5 kg zone A\");\n            Eq(RateCalculator.Quote(0.2m, Zone.A).Price, 5.00m, \"0.2 kg zone A\");\n            Eq(RateCalculator.Quote(1.0m, Zone.A).Price, 5.10m, \"1.0 kg zone A\");\n        });\n        Case(\"zone_parsing\", () =>\n        {\n            Eq(Zones.Parse(\"b\"), Zone.B, \"Parse(\\\"b\\\")\");\n            Eq(Zones.Parse(\" C \"), Zone.C, \"Parse(\\\" C \\\")\");\n            Eq(Zones.Parse(\"D\"), Zone.D, \"Parse(\\\"D\\\")\");\n            Throws<ArgumentException>(() => Zones.Parse(\"E\"), \"Parse(\\\"E\\\")\");\n            Throws<ArgumentException>(() => Zones.Parse(\"\"), \"Parse(\\\"\\\")\");\n        });\n        Case(\"label_with_company\", () =>\n        {\n            var got = LabelPrinter.Format(new Recipient(\" Ada Lovelace \", \" Analytical Engines Ltd\", \"12 St James's Square \", \" SW1Y 4JH London\", \"gb\"));\n            Eq(got, \"Ada Lovelace\\nAnalytical Engines Ltd\\n12 St James's Square\\nSW1Y 4JH London\\nGB\", \"label\");\n        });\n        Case(\"label_without_company\", () =>\n        {\n            const string want = \"Grace Hopper\\n1 Navy Way\\n20001 Washington\\nUS\";\n            Eq(LabelPrinter.Format(new Recipient(\"Grace Hopper\", null, \"1 Navy Way\", \"20001 Washington\", \"us\")), want, \"label with null company\");\n            Eq(LabelPrinter.Format(new Recipient(\"Grace Hopper\", \"   \", \"1 Navy Way\", \"20001 Washington\", \"US\")), want, \"label with blank company\");\n        });\n        Case(\"cli_quote\", () =>\n        {\n            var r = Cli(\"quote\", \"2.3\", \"b\");\n            Eq(r.Code, 0, \"exit code of `parcel quote 2.3 b`\");\n            Eq(r.Out.TrimEnd(), \"Zone B, 2.5 kg billable: 11.75 EUR\", \"stdout of `parcel quote 2.3 b`\");\n            r = Cli(\"quote\", \"0.3\", \"A\");\n            Eq(r.Out.TrimEnd(), \"Zone A, 0.5 kg billable: 5.00 EUR\", \"stdout of `parcel quote 0.3 A`\");\n        });\n        Case(\"cli_errors\", () =>\n        {\n            var r = Cli();\n            Eq(r.Code, 2, \"exit code with no arguments\");\n            Check(r.Err.Contains(\"usage\"), $\"no arguments: expected a usage line on stderr, got '{r.Err.Trim()}'\");\n            r = Cli(\"quote\", \"2\", \"x\");\n            Eq(r.Code, 1, \"exit code of `parcel quote 2 x`\");\n            Check(r.Err.StartsWith(\"error:\"), $\"`parcel quote 2 x`: expected stderr to start with 'error:', got '{r.Err.Trim()}'\");\n            r = Cli(\"quote\", \"31\", \"a\");\n            Eq(r.Code, 1, \"exit code of `parcel quote 31 a`\");\n            Eq(r.Out, \"\", \"stdout of `parcel quote 31 a`\");\n        });\n        Case(\"strict_build_settings\", () =>\n        {\n            var props = XDocument.Load(\"Directory.Build.props\");\n            string Prop(string n) => props.Descendants(n).LastOrDefault()?.Value.Trim() ?? \"\";\n            Eq(Prop(\"TreatWarningsAsErrors\").ToLowerInvariant(), \"true\", \"TreatWarningsAsErrors in Directory.Build.props\");\n            Eq(Prop(\"Nullable\").ToLowerInvariant(), \"enable\", \"Nullable in Directory.Build.props\");\n            foreach (var proj in Directory.GetFiles(\"src\", \"*.csproj\", SearchOption.AllDirectories))\n            {\n                var x = XDocument.Load(proj);\n                var name = Path.GetFileName(proj);\n                foreach (var el in x.Descendants())\n                {\n                    var n = el.Name.LocalName;\n                    var v = el.Value.Trim().ToLowerInvariant();\n                    Check(!(n == \"TreatWarningsAsErrors\" && v != \"true\"), $\"{name} turns TreatWarningsAsErrors off\");\n                    Check(!(n == \"Nullable\" && v != \"enable\"), $\"{name} sets Nullable to '{el.Value.Trim()}'\");\n                    Check(!((n == \"NoWarn\" || n == \"WarningsNotAsErrors\") && Regex.IsMatch(v, @\"cs86|nullable\")), $\"{name} silences nullable warnings ({n})\");\n                }\n            }\n            foreach (var cs in Directory.GetFiles(\"src\", \"*.cs\", SearchOption.AllDirectories).Where(p => !p.Contains($\"{Path.DirectorySeparatorChar}obj{Path.DirectorySeparatorChar}\")))\n            {\n                var text = File.ReadAllText(cs);\n                Check(!Regex.IsMatch(text, @\"#\\s*nullable\\s+disable|#\\s*pragma\\s+warning\\s+disable\"), $\"{Path.GetFileName(cs)} disables warnings or nullable checks\");\n            }\n        });\n        return 0;\n    }\n}\n",
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
    "Parcel.sln",
    "--nologo",
    "--disable-build-servers",
    "-nodeReuse:false",
    "-p:UseSharedCompilation=false",
    "-clp:NoSummary"
   ],
   "label": "dotnet build Parcel.sln",
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
   "label": "build the hidden tests (they use the public API in README.md)",
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
  "billable_weight_rounds_up",
  "weight_limits",
  "zone_prices",
  "minimum_charge",
  "zone_parsing",
  "label_with_company",
  "label_without_company",
  "cli_quote",
  "cli_errors",
  "strict_build_settings"
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
