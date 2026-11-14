import AppKit
import SwiftUI

// The app is started the plain AppKit way: one window that shows the SwiftUI
// view, and a menu bar with the usual Edit commands (copy and paste in the
// address box need them).
final class AppDelegate: NSObject, NSApplicationDelegate {
    private var window: NSWindow?
    private var tracer: Tracer?

    func applicationDidFinishLaunching(_ notification: Notification) {
        let selfTest = SelfTest.fromArguments()
        if selfTest != nil { SelfTest.log("launched") }
        let t = Tracer(selfTest: selfTest)
        tracer = t

        NSApp.mainMenu = makeMenu()
        let w = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 1200, height: 820),
                         styleMask: [.titled, .closable, .miniaturizable, .resizable],
                         backing: .buffered, defer: false)
        w.title = "ttrace"
        w.contentMinSize = NSSize(width: 900, height: 600)
        w.contentViewController = NSHostingController(rootView: ContentView(t: t))
        w.setContentSize(NSSize(width: 1200, height: 820))
        w.isReleasedWhenClosed = false
        if let screen = NSScreen.main?.visibleFrame, w.frame.width > screen.width || w.frame.height > screen.height {
            w.setFrame(screen, display: false)
        }
        w.center()
        w.setFrameAutosaveName("main")
        w.makeKeyAndOrderFront(nil)
        window = w

        NSApp.setActivationPolicy(.regular)
        NSApp.activate(ignoringOtherApps: true)
        if selfTest != nil { SelfTest.log("window shown: \(w.isVisible), windows: \(NSApp.windows.count)") }
        t.appeared()
    }

    func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool { true }

    func applicationWillTerminate(_ notification: Notification) {
        tracer?.stop()
    }

    private func makeMenu() -> NSMenu {
        let bar = NSMenu()

        let app = NSMenu()
        app.addItem(withTitle: "About ttrace", action: #selector(NSApplication.orderFrontStandardAboutPanel(_:)), keyEquivalent: "")
        app.addItem(.separator())
        app.addItem(withTitle: "Hide ttrace", action: #selector(NSApplication.hide(_:)), keyEquivalent: "h")
        let others = app.addItem(withTitle: "Hide Others", action: #selector(NSApplication.hideOtherApplications(_:)), keyEquivalent: "h")
        others.keyEquivalentModifierMask = [.command, .option]
        app.addItem(withTitle: "Show All", action: #selector(NSApplication.unhideAllApplications(_:)), keyEquivalent: "")
        app.addItem(.separator())
        app.addItem(withTitle: "Quit ttrace", action: #selector(NSApplication.terminate(_:)), keyEquivalent: "q")
        bar.addItem(withTitle: "ttrace", action: nil, keyEquivalent: "").submenu = app

        let edit = NSMenu(title: "Edit")
        edit.addItem(withTitle: "Undo", action: Selector(("undo:")), keyEquivalent: "z")
        let redo = edit.addItem(withTitle: "Redo", action: Selector(("redo:")), keyEquivalent: "z")
        redo.keyEquivalentModifierMask = [.command, .shift]
        edit.addItem(.separator())
        edit.addItem(withTitle: "Cut", action: #selector(NSText.cut(_:)), keyEquivalent: "x")
        edit.addItem(withTitle: "Copy", action: #selector(NSText.copy(_:)), keyEquivalent: "c")
        edit.addItem(withTitle: "Paste", action: #selector(NSText.paste(_:)), keyEquivalent: "v")
        edit.addItem(withTitle: "Select All", action: #selector(NSText.selectAll(_:)), keyEquivalent: "a")
        bar.addItem(withTitle: "Edit", action: nil, keyEquivalent: "").submenu = edit

        let win = NSMenu(title: "Window")
        win.addItem(withTitle: "Minimize", action: #selector(NSWindow.performMiniaturize(_:)), keyEquivalent: "m")
        win.addItem(withTitle: "Zoom", action: #selector(NSWindow.performZoom(_:)), keyEquivalent: "")
        win.addItem(.separator())
        win.addItem(withTitle: "Close", action: #selector(NSWindow.performClose(_:)), keyEquivalent: "w")
        bar.addItem(withTitle: "Window", action: nil, keyEquivalent: "").submenu = win
        NSApp.windowsMenu = win

        return bar
    }
}

@main
enum Main {
    static func main() {
        let app = NSApplication.shared
        let delegate = AppDelegate()
        app.delegate = delegate
        app.run()
    }
}
