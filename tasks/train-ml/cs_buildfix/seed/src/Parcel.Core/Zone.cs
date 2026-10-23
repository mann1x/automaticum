namespace Parcel.Core;

public enum Zone
{
    A,
    B,
    C,
    D,
}

public static class Zones
{
    public static Zone Parse(string name)
    {
        if (string.IsNullOrWhiteSpace(name))
        {
            throw new ArgumentException("zone is empty", nameof(name));
        }

        return name.Trim().ToUpperInvariant() switch
        {
            "A" => Zone.A,
            "B" => Zone.B,
            "C" => Zone.C,
            "D" => Zone.D,
            _ => throw new ArgumentException($"unknown zone '{name}'", nameof(name)),
        };
    }
}
