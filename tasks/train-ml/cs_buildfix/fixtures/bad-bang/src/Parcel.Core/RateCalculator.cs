namespace Parcel.Core;

public sealed record Quote(Zone Zone, decimal BillableKg, decimal Price);

public static class RateCalculator
{
    public const decimal MaxWeightKg = 30m;
    public const decimal MinimumCharge = 5.00m;

    private static readonly IReadOnlyDictionary<Zone, (decimal BaseFee, decimal PerKg)> Table =
        new Dictionary<Zone, (decimal, decimal)>
        {
            [Zone.A] = (3.90m, 1.20m),
            [Zone.B] = (6.50m, 2.10m),
            [Zone.C] = (9.00m, 3.40m),
            [Zone.D] = (14.00m, 6.80m),
        };

    public static decimal BillableWeight(decimal weightKg)
    {
        if (weightKg <= 0 || weightKg > MaxWeightKg)
        {
            throw new ArgumentOutOfRangeException(nameof(weightKg), weightKg, $"weight must be in (0, {MaxWeightKg}] kg");
        }

        var halves = Math.Ceiling(weightKg * 2);
        return Math.Max(halves / 2, 0.5m);
    }

    public static Quote Quote(decimal weightKg, Zone zone)
    {
        var billable = BillableWeight(weightKg);
        var (baseFee, perKg) = Table[zone];
        var price = Math.Round(baseFee + billable * perKg, 2, MidpointRounding.AwayFromZero);
        return new Quote(zone, billable, Math.Max(price, MinimumCharge));
    }
}
