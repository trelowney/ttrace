import AppKit
import SwiftUI

final class AppDelegate: NSObject, NSApplicationDelegate {
    func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool { true }

    func applicationDidFinishLaunching(_ notification: Notification) {
        // also when started straight from Contents/MacOS (as CI does)
        NSApp.setActivationPolicy(.regular)
        NSApp.activate(ignoringOtherApps: true)
    }
}

@main
struct TtraceApp: App {
    @NSApplicationDelegateAdaptor(AppDelegate.self) private var delegate
    @StateObject private var tracer = Tracer(selfTest: SelfTest.fromArguments())

    var body: some Scene {
        Window("ttrace", id: "main") {
            ContentView(t: tracer)
                .onAppear { tracer.appeared() }
        }
        .defaultSize(width: 1200, height: 820)
    }
}
