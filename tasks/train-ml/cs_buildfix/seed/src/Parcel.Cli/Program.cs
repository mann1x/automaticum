using System.Globalization;
using Parcel.Core;

namespace Parcel.Cli;

public static class Program
{
    private const string Usage = "usage: parcel quote <weightKg> <zone>";

    public static int Main(string[] args)
    {
        if (args.Length != 3 || args[0] != "quote")
        {
            Console.Error.WriteLine(Usage);
            return 2;
        }

        try
        {
            if (!decimal.TryParse(args[1], NumberStyles.Number, CultureInfo.InvariantCulture, out var weight))
            {
                throw new ArgumentException($"not a weight: '{args[1]}'");
            }

            var quote = RateCalculator.Quote(weight, Zones.Parse(args[2]));
            Console.WriteLine(string.Format(
                CultureInfo.InvariantCulture,
                "Zone {0}, {1:0.0} kg billable: {2:0.00} EUR",
                quote.Zone,
                quote.BillableKg,
                quote.Price));
            return 0;
        }
        catch (ArgumentException e)
        {
            Console.Error.WriteLine($"error: {e.Message}");
            return 1;
        }
    }
}
