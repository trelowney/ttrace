using System.Collections.Generic;
using System.ComponentModel;
using System.Globalization;
using System.Runtime.CompilerServices;
using System.Text.Json.Serialization;

namespace TtraceGui;

// One line of "ttrace --json-stream": the state after a round.
public sealed class Snapshot
{
    [JsonPropertyName("target")] public string Target { get; set; } = "";
    [JsonPropertyName("ip")] public string Ip { get; set; } = "";
    [JsonPropertyName("family")] public string Family { get; set; } = "";
    [JsonPropertyName("probe")] public string Probe { get; set; } = "";
    [JsonPropertyName("round")] public int Round { get; set; }
    [JsonPropertyName("reached")] public bool Reached { get; set; }
    [JsonPropertyName("hops")] public List<Hop> Hops { get; set; } = new();
    [JsonPropertyName("geo")] public string Geo { get; set; } = "";
}

public sealed class Hop
{
    [JsonPropertyName("ttl")] public int Ttl { get; set; }
    [JsonPropertyName("addr")] public string Addr { get; set; } = "";
    [JsonPropertyName("host")] public string Host { get; set; } = "";
    [JsonPropertyName("asn")] public string Asn { get; set; } = "";
    [JsonPropertyName("as_name")] public string AsName { get; set; } = "";
    [JsonPropertyName("country")] public string Country { get; set; } = "";
    [JsonPropertyName("private")] public bool Private { get; set; }
    [JsonPropertyName("hidden")] public bool Hidden { get; set; }
    [JsonPropertyName("sent")] public int Sent { get; set; }
    [JsonPropertyName("recv")] public int Recv { get; set; }
    [JsonPropertyName("loss_pct")] public double Loss { get; set; }
    [JsonPropertyName("last_ms")] public double Last { get; set; }
    [JsonPropertyName("avg_ms")] public double Avg { get; set; }
    [JsonPropertyName("best_ms")] public double Best { get; set; }
    [JsonPropertyName("worst_ms")] public double Worst { get; set; }
    [JsonPropertyName("stdev_ms")] public double StDev { get; set; }
    [JsonPropertyName("place")] public Place? Place { get; set; }
}

public sealed class Place
{
    [JsonPropertyName("city")] public string City { get; set; } = "";
    [JsonPropertyName("region")] public string Region { get; set; } = "";
    [JsonPropertyName("country")] public string Country { get; set; } = "";
    [JsonPropertyName("lat")] public double Lat { get; set; }
    [JsonPropertyName("lon")] public double Lon { get; set; }

    public string Name =>
        City != "" && Country != "" ? $"{City}, {Country}" : City != "" ? City : Country;
}

// HopRow is one line of the table. It is updated in place every round so
// the list does not jump or lose the selection.
public sealed class HopRow : INotifyPropertyChanged
{
    public event PropertyChangedEventHandler? PropertyChanged;

    private string ttl = "", host = "", asn = "", asName = "", country = "", place = "";
    private string loss = "", sent = "", last = "", avg = "", best = "", worst = "";

    public string Ttl { get => ttl; set => Set(ref ttl, value); }
    public string Host { get => host; set => Set(ref host, value); }
    public string Asn { get => asn; set => Set(ref asn, value); }
    public string AsName { get => asName; set => Set(ref asName, value); }
    public string Country { get => country; set => Set(ref country, value); }
    public string PlaceName { get => place; set => Set(ref place, value); }
    public string Loss { get => loss; set => Set(ref loss, value); }
    public string Sent { get => sent; set => Set(ref sent, value); }
    public string Last { get => last; set => Set(ref last, value); }
    public string Avg { get => avg; set => Set(ref avg, value); }
    public string Best { get => best; set => Set(ref best, value); }
    public string Worst { get => worst; set => Set(ref worst, value); }

    public void Update(Hop h)
    {
        var c = CultureInfo.CurrentCulture;
        Ttl = h.Ttl.ToString(c);
        Host = h.Hidden ? "(hidden)" : h.Addr == "" ? "no answer" : h.Host != "" ? $"{h.Host}  ({h.Addr})" : h.Addr;
        Asn = h.Private ? "private" : h.Asn != "" ? "AS" + h.Asn : "";
        AsName = h.AsName;
        Country = h.Country;
        PlaceName = h.Place?.Name ?? "";
        Loss = h.Sent > 0 ? h.Loss.ToString("0.0", c) + " %" : "";
        Sent = h.Sent.ToString(c);
        bool got = h.Recv > 0;
        Last = got ? h.Last.ToString("0.0", c) : "";
        Avg = got ? h.Avg.ToString("0.0", c) : "";
        Best = got ? h.Best.ToString("0.0", c) : "";
        Worst = got ? h.Worst.ToString("0.0", c) : "";
    }

    private void Set(ref string field, string value, [CallerMemberName] string? name = null)
    {
        if (field == value) return;
        field = value;
        PropertyChanged?.Invoke(this, new PropertyChangedEventArgs(name));
    }
}
