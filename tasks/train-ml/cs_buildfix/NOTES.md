# cs_buildfix notes

- Planted (build): Parcel.Core targets net9.0 (NETSDK1045 on the 8.0 SDK); Parcel.Core has an explicit
  `<Compile Include="**/*.cs">` on top of the SDK's default items (NETSDK1022 duplicates, shows up only after
  the TFM fix); Parcel.Cli's ProjectReference points at `..\Core\...` (project not found -> CS0246).
- Planted (code): billable weight rounds to the NEAREST half kg instead of UP (prices too low);
  `LabelPrinter` calls `r.Company.Trim()` on a `string?` (CS8602 -> error under warnings-as-errors, NRE at run time).
- Reference: net8.0, drop the Compile item, fix the reference path, `Math.Ceiling(w*2)/2`, `r.Company?.Trim()` +
  IsNullOrEmpty. `strict_build_settings` fails a "fix" that turns off TreatWarningsAsErrors/Nullable, adds NoWarn CS86xx,
  or uses `#nullable disable` / `#pragma warning disable`.
- Near-misses: `round-nearest` (all builds fixed, rounding left) and `null-forgiving` (`r.Company!.Trim()`:
  compiles, NREs on a null company).
- Offline .NET recipe: no PackageReference anywhere; HOME/DOTNET_CLI_HOME under `$WORK/__checker__/home`;
  the hidden empty file `home/.local/share/NuGet/Migrations/1` is REQUIRED: on this host's docker, .NET named
  mutexes fail ("The system cannot open the device or file specified : 'NuGet-Migrations'", even as root without
  --read-only), and NuGet only takes that mutex when the migration marker is missing. `--disable-build-servers
  -nodeReuse:false -p:UseSharedCompilation=false` so no compiler server outlives the step. Tests run as
  `dotnet __checker__/bin/Debug/net8.0/Checker.dll` (no apphost): passes verify both with the default exec /tmp and with `--tmp-noexec`.
- An agent working this task in the same image will hit the NuGet-Migrations crash on its own `dotnet build`
  unless its HOME has that marker (or the lane fixes named mutexes).
