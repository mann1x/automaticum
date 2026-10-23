# Parcel

Parcel quotes shipping prices for our courier desk and prints address labels.

```
src/Parcel.Core   class library: zones, rate table, labels
src/Parcel.Cli    console front end (`parcel`)
```

Build and run (.NET 8 SDK, no NuGet packages):

```
dotnet build Parcel.sln
dotnet src/Parcel.Cli/bin/Debug/net8.0/Parcel.Cli.dll quote 2.3 b
```

Both projects target **net8.0**. Warnings are errors and nullable reference types are on for the whole
repository (`Directory.Build.props`); keep it that way -- fix warnings in the code, do not silence them.

## Rates

Zones: `A` (domestic), `B` (neighbouring countries), `C` (rest of Europe), `D` (world). Zone names are
case-insensitive (`b` == `B`); anything else is an error.

| zone | base fee | per billable kg |
|------|---------:|----------------:|
| A    |     3.90 |            1.20 |
| B    |     6.50 |            2.10 |
| C    |     9.00 |            3.40 |
| D    |    14.00 |            6.80 |

* **Billable weight** is the actual weight rounded **up** to the next half kilogram: 2.0 kg bills as 2.0,
  2.01 kg and 2.3 kg bill as 2.5, 2.6 kg bills as 3.0. Every parcel bills at least 0.5 kg.
* Weight must be greater than 0 and at most 30 kg (`ArgumentOutOfRangeException` otherwise).
* price = base fee + billable kg x per-kg rate, rounded to cents (midpoint away from zero).
* Prices are never below the minimum charge of **5.00** (a zone A parcel of 0.5 kg costs 5.00, not 4.50).

`RateCalculator.Quote(decimal weightKg, Zone zone)` returns a `Quote` with `BillableKg` and `Price`.
`Zones.Parse(string)` turns a zone name into a `Zone` (throws `ArgumentException` for unknown names).

## Labels

`LabelPrinter.Format(Recipient r)` returns the label text, one line per field, joined with `\n`:

```
Ada Lovelace
Analytical Engines Ltd      <- only when the recipient has a company (null or blank: no line)
12 St James's Square
SW1Y 4JH London
GB
```

Name, street and city lines are trimmed. The country code is printed in upper case.

## CLI

```
parcel quote <weightKg> <zone>
```

prints exactly one line, e.g. `Zone B, 2.5 kg billable: 11.75 EUR`
(weight with one decimal, price with two decimals, invariant culture), exit code 0.

Wrong usage (missing/extra arguments, unknown command) prints a usage line to stderr and exits 2.
An invalid weight or zone prints `error: <message>` to stderr and exits 1.
