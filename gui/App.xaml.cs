using System;
using System.IO;
using Microsoft.UI.Xaml;

namespace TtraceGui;

public partial class App : Application
{
    private Window? window;

    public App()
    {
        InitializeComponent();
        UnhandledException += (_, e) => Crash.Write(e.Exception);
        // XAML errors say only "XAML parsing failed"; these say what exactly
        DebugSettings.IsXamlResourceReferenceTracingEnabled = true;
        DebugSettings.IsBindingTracingEnabled = true;
        DebugSettings.XamlResourceReferenceFailed += (_, e) => Crash.Write(new Exception("XAML resource: " + e.Message));
        DebugSettings.BindingFailed += (_, e) => Crash.Write(new Exception("XAML binding: " + e.Message));
    }

    protected override void OnLaunched(LaunchActivatedEventArgs args)
    {
        var argv = Environment.GetCommandLineArgs(); // [0] is the program itself
        SelfTest? test = argv.Length >= 4 && argv[1] == "--selftest" ? new SelfTest(argv[2], argv[3]) : null;
        window = new MainWindow(test);
        window.Activate();
    }
}

// Crash writes what went wrong into a file, so a crash can be reported.
static class Crash
{
    public static string FilePath => Path.Combine(
        Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData), "ttrace", "gui-crash.txt");

    public static void Write(Exception ex)
    {
        try
        {
            Directory.CreateDirectory(Path.GetDirectoryName(FilePath)!);
            File.AppendAllText(FilePath, $"{DateTime.Now:u}\n{ex}\n\n");
        }
        catch
        {
            // nothing more we can do
        }
    }
}
