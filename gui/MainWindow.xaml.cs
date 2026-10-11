using System;
using System.Collections.Generic;
using System.Collections.ObjectModel;
using System.Diagnostics;
using System.Globalization;
using System.Runtime.InteropServices;
using System.Text;
using System.Text.Json;
using Microsoft.UI.Dispatching;
using Microsoft.UI.Windowing;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;
using Microsoft.UI.Xaml.Input;
using Microsoft.UI.Xaml.Media;
using Microsoft.UI.Xaml.Shapes;
using Windows.Foundation;

namespace TtraceGui;

// SelfTest is "ttrace-gui --selftest <result file> <target>": trace the
// target, write one line about how it went into the file and quit. CI uses
// it to check that the window, ttrace.exe and the JSON reading work together.
public sealed record SelfTest(string ResultFile, string Target);

public sealed partial class MainWindow : Window
{
    public ObservableCollection<HopRow> Rows { get; } = new();

    private readonly SelfTest? selfTest;
    private readonly DispatcherQueue dq;
    private Process? proc;
    private int runId;                 // output of older (stopped) runs is ignored
    private readonly List<string> errLines = new();
    private Snapshot? last;
    private Action? infoAction;        // what the button in the message bar does
    private bool selfTestFinished;

    // land outline: rings of (lon, lat), loaded once from Assets\land.bin
    private List<Point[]>? land;
    private string landError = "";

    private static readonly JsonSerializerOptions JsonOpts = new() { PropertyNameCaseInsensitive = true };

    public MainWindow(SelfTest? selfTest = null)
    {
        this.selfTest = selfTest;
        InitializeComponent();
        dq = DispatcherQueue;
        SetSize(1200, 860);
        Closed += (_, _) => Stop();

        LoadLand();
        ShowIdle();

        if (selfTest != null)
        {
            TargetBox.Text = selfTest.Target;
            var timer = dq.CreateTimer();
            timer.Interval = TimeSpan.FromSeconds(90);
            timer.IsRepeating = false;
            timer.Tick += (_, _) => SelfTestDone($"timeout round={last?.Round ?? 0} hops={Rows.Count}");
            timer.Start();
            Start();
        }
    }

    // ---- starting and stopping ttrace.exe ----

    private void StartButton_Click(object sender, RoutedEventArgs e)
    {
        if (proc != null) StopByUser();
        else Start();
    }

    private void TargetBox_KeyDown(object sender, KeyRoutedEventArgs e)
    {
        if (e.Key != Windows.System.VirtualKey.Enter) return;
        e.Handled = true;
        Start();
    }

    private void FamilyBox_SelectionChanged(object sender, SelectionChangedEventArgs e)
    {
        // a running trace switches to the chosen IP version right away
        if (proc != null) Start();
    }

    private void InfoAction_Click(object sender, RoutedEventArgs e)
    {
        Info.IsOpen = false;
        infoAction?.Invoke();
    }

    private void MapRetry_Click(object sender, RoutedEventArgs e) => Start();

    private void Start()
    {
        string target = TargetBox.Text.Trim();
        if (target == "")
        {
            ShowInfo(InfoBarSeverity.Informational, "What should be traced?",
                "Type the address you want to trace into the box above, for example example.com or 1.1.1.1, then press Start.",
                null, null);
            TargetBox.Focus(FocusState.Programmatic);
            return;
        }

        Stop();
        string exe = System.IO.Path.Combine(AppContext.BaseDirectory, "ttrace.exe");
        if (!System.IO.File.Exists(exe))
        {
            Fail("ttrace.exe is missing",
                $"This program carries ttrace.exe inside and unpacks it into {AppContext.BaseDirectory}, but it is not there " +
                "(antivirus software may have removed it). Allow ttrace there, or download this program again, and try again.");
            return;
        }

        var psi = new ProcessStartInfo(exe)
        {
            UseShellExecute = false,
            CreateNoWindow = true,
            RedirectStandardOutput = true,
            RedirectStandardError = true,
            StandardOutputEncoding = Encoding.UTF8,
            StandardErrorEncoding = Encoding.UTF8,
        };
        psi.ArgumentList.Add("--json-stream");
        if (FamilyBox.SelectedIndex == 1) psi.ArgumentList.Add("-4");
        if (FamilyBox.SelectedIndex == 2) psi.ArgumentList.Add("-6");
        psi.ArgumentList.Add("--"); // the target is never read as an option, even if it starts with "-"
        psi.ArgumentList.Add(target);

        var p = new Process { StartInfo = psi, EnableRaisingEvents = true };
        int id = ++runId;
        p.OutputDataReceived += (_, e) => { if (e.Data is string l) dq.TryEnqueue(() => OnLine(id, l)); };
        p.ErrorDataReceived += (_, e) => { if (e.Data is string l) dq.TryEnqueue(() => OnErrLine(id, l)); };
        p.Exited += (_, _) =>
        {
            int code;
            try
            {
                p.WaitForExit(); // also waits until all output has been read
                code = p.ExitCode;
            }
            catch
            {
                return; // stopped and cleaned up already
            }
            dq.TryEnqueue(() => OnExited(id, code));
        };

        try
        {
            p.Start();
            p.BeginOutputReadLine();
            p.BeginErrorReadLine();
        }
        catch (Exception ex)
        {
            p.Dispose();
            Fail("ttrace.exe could not be started",
                ex.Message + " — if antivirus software blocked it, allow ttrace.exe there and try again.");
            return;
        }

        proc = p;
        errLines.Clear();
        last = null;
        Rows.Clear();
        Info.IsOpen = false;
        StartButton.Content = "Stop";
        Summary.Text = $"Tracing {target}…";
        Redraw();
    }

    // Stop ends the running ttrace.exe quietly.
    private void Stop()
    {
        if (proc == null) return;
        runId++; // ignore whatever the old process still sends
        try
        {
            if (!proc.HasExited) proc.Kill(entireProcessTree: true);
        }
        catch
        {
            // it has just ended by itself
        }
        proc.Dispose();
        proc = null;
        StartButton.Content = "Start";
    }

    private void StopByUser()
    {
        Stop();
        if (last != null) Summary.Text = SummaryText(last) + " · stopped";
        else Summary.Text = "";
        Redraw();
    }

    // ---- what ttrace.exe sends ----

    private void OnLine(int id, string line)
    {
        if (id != runId) return;
        Snapshot? s;
        try
        {
            s = JsonSerializer.Deserialize<Snapshot>(line, JsonOpts);
        }
        catch (JsonException)
        {
            return; // not a result line
        }
        if (s == null) return;
        s.Hops ??= new();
        s.Geo ??= "";
        last = s;

        for (int i = 0; i < s.Hops.Count; i++)
        {
            if (i < Rows.Count)
            {
                Rows[i].Update(s.Hops[i]);
            }
            else
            {
                var r = new HopRow();
                r.Update(s.Hops[i]);
                Rows.Add(r);
            }
        }
        while (Rows.Count > s.Hops.Count) Rows.RemoveAt(Rows.Count - 1);

        Summary.Text = SummaryText(s);
        Redraw();

        if (selfTest != null)
        {
            int places = 0;
            foreach (var h in s.Hops) if (h.Place != null) places++;
            if ((places > 0 && s.Round >= 3) || s.Round >= 10)
                SelfTestDone($"ok hops={s.Hops.Count} places={places} round={s.Round}");
        }
    }

    private void OnErrLine(int id, string line)
    {
        if (id != runId || line.Trim() == "") return;
        errLines.Add(line.StartsWith("ttrace: ", StringComparison.Ordinal) ? line["ttrace: ".Length..] : line);
    }

    private void OnExited(int id, int code)
    {
        if (id != runId) return;
        proc?.Dispose();
        proc = null;
        StartButton.Content = "Start";
        if (code != 0)
        {
            string msg = errLines.Count > 0
                ? string.Join(" ", errLines)
                : $"ttrace.exe stopped unexpectedly (exit code {code}).";
            Fail("The trace could not run", msg + " Check the address and the internet connection, then try again.");
        }
        else
        {
            ShowInfo(InfoBarSeverity.Informational, "The trace has ended", "Press Start to run it again.", "Start again", Start);
        }
        if (last != null) Summary.Text = SummaryText(last) + " · ended";
        Redraw();
    }

    private static string SummaryText(Snapshot s)
    {
        var sb = new StringBuilder(s.Target);
        if (s.Ip != "" && s.Ip != s.Target) sb.Append($" ({s.Ip})");
        if (s.Family != "") sb.Append(" · ").Append(s.Family);
        if (s.Probe != "") sb.Append(" · ").Append(s.Probe);
        sb.Append(" · round ").Append(s.Round.ToString(CultureInfo.CurrentCulture));
        sb.Append(s.Reached ? " · target reached" : " · target not reached yet");
        return sb.ToString();
    }

    // ---- messages ----

    private void Fail(string title, string message)
    {
        ShowInfo(InfoBarSeverity.Error, title, message, "Try again", Start);
        if (selfTest != null) SelfTestDone("error: " + title + ": " + message);
    }

    private void ShowInfo(InfoBarSeverity severity, string title, string message, string? action, Action? onAction)
    {
        Info.Severity = severity;
        Info.Title = title;
        Info.Message = message;
        infoAction = onAction;
        InfoAction.Content = action ?? "";
        InfoAction.Visibility = action == null ? Visibility.Collapsed : Visibility.Visible;
        Info.IsOpen = true;
    }

    private void ShowIdle()
    {
        Summary.Text = "";
        Redraw();
    }

    private void SelfTestDone(string result)
    {
        if (selfTestFinished) return;
        selfTestFinished = true;
        try
        {
            System.IO.File.WriteAllText(selfTest!.ResultFile, result + Environment.NewLine);
        }
        catch (Exception ex)
        {
            Crash.Write(ex);
        }
        Stop();
        Application.Current.Exit();
    }

    // ---- window size ----

    [DllImport("user32.dll")]
    private static extern uint GetDpiForWindow(IntPtr hwnd);

    private void SetSize(int width, int height)
    {
        try
        {
            IntPtr hwnd = WinRT.Interop.WindowNative.GetWindowHandle(this);
            double scale = GetDpiForWindow(hwnd) / 96.0;
            if (scale <= 0) scale = 1;
            var area = DisplayArea.GetFromWindowId(AppWindow.Id, DisplayAreaFallback.Primary).WorkArea;
            int w = Math.Min((int)(width * scale), area.Width);
            int h = Math.Min((int)(height * scale), area.Height);
            AppWindow.MoveAndResize(new Windows.Graphics.RectInt32(
                area.X + (area.Width - w) / 2, area.Y + (area.Height - h) / 2, w, h));
        }
        catch (Exception ex)
        {
            Crash.Write(ex); // the default size is fine too
        }
    }

    // ---- table empty text and map ----

    private void MapCanvas_SizeChanged(object sender, SizeChangedEventArgs e) => Redraw();

    private void LoadLand()
    {
        try
        {
            string file = System.IO.Path.Combine(AppContext.BaseDirectory, "Assets", "land.bin");
            using var r = new System.IO.BinaryReader(System.IO.File.OpenRead(file));
            int rings = r.ReadInt32();
            var list = new List<Point[]>(rings);
            for (int i = 0; i < rings; i++)
            {
                int n = r.ReadInt32();
                var pts = new Point[n];
                for (int j = 0; j < n; j++)
                {
                    double lon = r.ReadInt16() / 100.0;
                    double lat = r.ReadInt16() / 100.0;
                    pts[j] = new Point(lon, lat);
                }
                list.Add(pts);
            }
            land = list;
        }
        catch (Exception ex)
        {
            landError = "The world outline could not be loaded, so only the route is drawn. Downloading this program again may help.";
            Crash.Write(ex);
            return;
        }

        // Three copies side by side, so routes across the Pacific have land
        // on both sides. A geometry can belong to only one Path.
        LandWest.Data = LandGeometry(-360);
        LandMid.Data = LandGeometry(0);
        LandEast.Data = LandGeometry(360);
    }

    private PathGeometry LandGeometry(double shift)
    {
        var g = new PathGeometry();
        foreach (var ring in land!)
        {
            if (ring.Length < 3) continue;
            var seg = new PolyLineSegment();
            for (int j = 1; j < ring.Length; j++)
                seg.Points.Add(new Point(ring[j].X + shift, ring[j].Y));
            var fig = new PathFigure
            {
                StartPoint = new Point(ring[0].X + shift, ring[0].Y),
                IsClosed = true,
                IsFilled = true,
            };
            fig.Segments.Add(seg);
            g.Figures.Add(fig);
        }
        return g;
    }

    // A place on the map: consecutive hops at the same spot share one marker.
    private sealed record Group(int First, int LastTtl, Place Place, string Host, double Lon);

    private void Redraw()
    {
        // table placeholder
        if (Rows.Count > 0)
        {
            TableEmpty.Visibility = Visibility.Collapsed;
        }
        else
        {
            TableEmpty.Visibility = Visibility.Visible;
            TableEmpty.Text = proc != null
                ? "Sending the first probes… the routers on the way appear here in a moment."
                : "Type an address above and press Start. Every router on the way to it will be listed here.";
        }

        double w = MapCanvas.ActualWidth, h = MapCanvas.ActualHeight;
        if (w < 10 || h < 10) return;
        MapCanvas.Clip = new RectangleGeometry { Rect = new Rect(0, 0, w, h) };

        // places of the hops, with longitudes unwrapped so a route over the
        // Pacific stays connected instead of crossing the whole world
        var groups = new List<Group>();
        var s = last;
        int lastTtl = s != null && s.Hops.Count > 0 ? s.Hops[^1].Ttl : -1;
        if (s != null)
        {
            foreach (var hop in s.Hops)
            {
                if (hop.Place == null) continue;
                if (groups.Count > 0 && groups[^1].Place.Lat == hop.Place.Lat && groups[^1].Place.Lon == hop.Place.Lon)
                {
                    groups[^1] = groups[^1] with { LastTtl = hop.Ttl };
                    continue;
                }
                double lon = hop.Place.Lon;
                if (groups.Count > 0)
                {
                    double prev = groups[^1].Lon;
                    while (lon - prev > 180) lon -= 360;
                    while (lon - prev < -180) lon += 360;
                }
                string host = hop.Host != "" ? hop.Host : hop.Addr;
                groups.Add(new Group(hop.Ttl, hop.Ttl, hop.Place, host, lon));
            }
        }

        // view: all places with some margin, or the whole world
        double minLat = 90, maxLat = -90, minLon = 1e9, maxLon = -1e9;
        foreach (var g in groups)
        {
            minLat = Math.Min(minLat, g.Place.Lat); maxLat = Math.Max(maxLat, g.Place.Lat);
            minLon = Math.Min(minLon, g.Lon); maxLon = Math.Max(maxLon, g.Lon);
        }
        if (groups.Count == 0) { minLat = -58; maxLat = 78; minLon = -170; maxLon = 190; }
        double midLat = (minLat + maxLat) / 2;
        double k = Math.Max(Math.Cos(midLat * Math.PI / 180), 0.2); // shrink east-west away from the equator
        double cx = (minLon + maxLon) / 2 * k, cy = midLat;
        double spanX = Math.Max((maxLon - minLon) * k * 1.25, 16);
        double spanY = Math.Max((maxLat - minLat) * 1.25, 9);
        double scale = Math.Min(w / spanX, h / spanY); // pixels per degree

        var view = new Matrix(k * scale, 0, 0, -scale, -cx * scale + w / 2, cy * scale + h / 2);
        foreach (var copy in new[] { LandWest, LandMid, LandEast })
        {
            if (copy.Data != null) copy.Data.Transform = new MatrixTransform { Matrix = view }; // one transform per geometry
        }

        Point ToScreen(double lat, double lon) => new((lon * k - cx) * scale + w / 2, (cy - lat) * scale + h / 2);

        var line = new PointCollection();
        foreach (var g in groups) line.Add(ToScreen(g.Place.Lat, g.Lon));
        RouteLine.Points = line;

        Markers.Children.Clear();
        Brush routeBrush = RouteLine.Stroke;
        Brush targetBrush = TargetBrushSource.Fill;
        foreach (var g in groups)
        {
            var p = ToScreen(g.Place.Lat, g.Lon);
            bool isTarget = s != null && s.Reached && g.LastTtl == lastTtl;
            string hops = g.First == g.LastTtl
                ? g.First.ToString(CultureInfo.CurrentCulture)
                : $"{g.First}–{g.LastTtl}";
            string tip = $"{(g.First == g.LastTtl ? "Hop" : "Hops")} {hops}: {g.Place.Name} (approximate)\n{g.Host}";
            if (isTarget) tip += "\nThis is the target.";

            var dot = new Ellipse { Width = 12, Height = 12, Fill = isTarget ? targetBrush : routeBrush };
            Canvas.SetLeft(dot, p.X - 6);
            Canvas.SetTop(dot, p.Y - 6);
            ToolTipService.SetToolTip(dot, tip);
            Markers.Children.Add(dot);

            var label = new TextBlock { Text = hops, FontSize = 12, FontWeight = Microsoft.UI.Text.FontWeights.SemiBold };
            Canvas.SetLeft(label, p.X + 8);
            Canvas.SetTop(label, p.Y - 9);
            ToolTipService.SetToolTip(label, tip);
            Markers.Children.Add(label);
        }

        // note in the corner of the map: always says what is going on and what to do
        string note = "";
        bool retry = false;
        if (landError != "")
        {
            note = landError;
        }
        else if (s == null)
        {
            note = proc != null
                ? "The route is drawn here as soon as the first answers arrive."
                : "The route will be drawn here on a world map once a trace runs.";
        }
        else if (s.Geo.StartsWith("The location data for the map could not be downloaded", StringComparison.Ordinal))
        {
            note = s.Geo + (groups.Count > 0
                ? " The older data is used meanwhile."
                : " The table works without it. Press Try again to download it again.");
            retry = true;
        }
        else if (s.Geo != "")
        {
            note = s.Geo + " — this happens once. The map fills in by itself when it is done; the table already works.";
        }
        else if (groups.Count == 0)
        {
            note = "No router on this route has a known place yet. Private addresses (like your home network) " +
                   "and routers that do not answer cannot be placed on the map.";
        }
        MapNote.Text = note;
        MapRetry.Visibility = retry ? Visibility.Visible : Visibility.Collapsed;
        MapNoteBox.Visibility = note == "" ? Visibility.Collapsed : Visibility.Visible;
    }
}
