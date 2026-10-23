namespace Parcel.Core;

public sealed record Recipient(string Name, string? Company, string Street, string City, string Country);

public static class LabelPrinter
{
    public static string Format(Recipient r)
    {
        var lines = new List<string> { r.Name.Trim() };
        var company = r.Company.Trim();
        if (company.Length > 0)
        {
            lines.Add(company);
        }

        lines.Add(r.Street.Trim());
        lines.Add(r.City.Trim());
        lines.Add(r.Country.Trim().ToUpperInvariant());
        return string.Join("\n", lines);
    }
}
