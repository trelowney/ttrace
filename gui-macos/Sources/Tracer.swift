import AppKit
import Foundation

// SelfTest is "ttrace-gui --selftest <result file> <target>": trace the
// target, write one line about how it went into the file, save a picture of
// the window next to it (<result file>.png) and quit. CI uses it to check
// that the window, the bundled ttrace and the JSON reading work together.
struct SelfTest {
    let resultFile: String
    let target: String

    // log writes a line to stderr during a self-test, so CI shows how far it got.
    static func log(_ line: String) {
        FileHandle.standardError.write(Data(("selftest: " + line + "\n").utf8))
    }

    static func fromArguments() -> SelfTest? {
        let a = CommandLine.arguments
        guard let i = a.firstIndex(of: "--selftest"), i + 2 < a.count else { return nil }
        return SelfTest(resultFile: a[i + 1], target: a[i + 2])
    }
}

// Banner is a message above the table. It always has a way out: the Close
// button, and for errors a button that tries again.
struct Banner {
    enum Kind { case info, error }
    let kind: Kind
    let title: String
    let message: String
    let action: String?
    let run: (() -> Void)?
}

// Tracer runs the bundled ttrace with --json-stream and keeps what it sends.
// Everything here is used on the main thread only.
final class Tracer: ObservableObject {
    @Published var target = ""
    @Published var family = 0 // 0 automatic, 1 IPv4 only, 2 IPv6 only
    @Published private(set) var running = false
    @Published private(set) var last: Snapshot?
    @Published private(set) var rows: [HopRow] = []
    @Published private(set) var summary = ""
    @Published var banner: Banner?

    let selfTest: SelfTest?
    private var proc: Process?
    private var runId = 0 // output of older (stopped) runs is ignored
    private var errLines: [String] = []
    private var outBuf = Data()
    private var errBuf = Data()
    private var selfTestFinished = false
    private var started = false

    init(selfTest: SelfTest?) {
        self.selfTest = selfTest
        if let t = selfTest {
            SelfTest.log("started")
            // a last resort that does not need the main thread: if the window
            // never comes up or hangs, still leave a result and quit
            DispatchQueue.global().asyncAfter(deadline: .now() + 170) {
                SelfTest.log("no result after 170 s")
                try? "timeout: the window did not finish\n".write(toFile: t.resultFile, atomically: true, encoding: .utf8)
                exit(1)
            }
            DispatchQueue.main.asyncAfter(deadline: .now() + 20) { [weak self] in
                // the window should be up by now; start anyway if it is not
                guard let self = self, !self.started else { return }
                SelfTest.log("window did not appear in 20 s (windows: \(NSApp.windows.count)), starting anyway")
                self.appeared()
            }
        }
        NotificationCenter.default.addObserver(forName: NSApplication.willTerminateNotification, object: nil, queue: .main) { [weak self] _ in
            self?.stop()
        }
    }

    // appeared is called once the window is on the screen.
    func appeared() {
        guard !started else { return }
        started = true
        guard let t = selfTest else { return }
        SelfTest.log("window appeared, windows: \(NSApp.windows.count)")
        target = t.target
        DispatchQueue.main.asyncAfter(deadline: .now() + 150) { [weak self] in
            guard let self = self else { return }
            self.selfTestDone("timeout round=\(self.last?.round ?? 0) hops=\(self.rows.count)")
        }
        start()
    }

    static var helper: URL? {
        Bundle.main.executableURL?.deletingLastPathComponent().appendingPathComponent("ttrace")
    }

    func start() {
        let t = target.trimmingCharacters(in: .whitespacesAndNewlines)
        if t.isEmpty {
            banner = Banner(kind: .info, title: "What should be traced?",
                            message: "Type the address you want to trace into the box above, for example example.com or 1.1.1.1, then press Start.",
                            action: nil, run: nil)
            return
        }

        stop()
        guard let exe = Tracer.helper, FileManager.default.isExecutableFile(atPath: exe.path) else {
            fail("ttrace is missing inside the app",
                 "The app carries the measuring program ttrace inside, but it is not there. Download the app again, unpack it and try again.")
            return
        }

        let p = Process()
        p.executableURL = exe
        var args = ["--json-stream"]
        if family == 1 { args.append("-4") }
        if family == 2 { args.append("-6") }
        args.append("--") // the target is never read as an option, even if it starts with "-"
        args.append(t)
        p.arguments = args
        let out = Pipe(), err = Pipe()
        p.standardOutput = out
        p.standardError = err
        p.standardInput = FileHandle.nullDevice

        runId += 1
        let id = runId
        out.fileHandleForReading.readabilityHandler = { h in
            let d = h.availableData
            if d.isEmpty { h.readabilityHandler = nil; return }
            DispatchQueue.main.async { self.gotOut(id, d) }
        }
        err.fileHandleForReading.readabilityHandler = { h in
            let d = h.availableData
            if d.isEmpty { h.readabilityHandler = nil; return }
            DispatchQueue.main.async { self.gotErr(id, d) }
        }
        p.terminationHandler = { p in
            // take what is still in the pipes, then report the end
            out.fileHandleForReading.readabilityHandler = nil
            err.fileHandleForReading.readabilityHandler = nil
            let restOut = out.fileHandleForReading.readDataToEndOfFile()
            let restErr = err.fileHandleForReading.readDataToEndOfFile()
            let code = p.terminationStatus
            let signaled = p.terminationReason == .uncaughtSignal
            DispatchQueue.main.async {
                self.gotOut(id, restOut)
                self.gotErr(id, restErr)
                self.exited(id, code: code, signaled: signaled)
            }
        }

        do {
            try p.run()
        } catch {
            fail("ttrace could not be started",
                 error.localizedDescription + " Download the app again, unpack it and try again.")
            return
        }

        proc = p
        errLines = []
        outBuf = Data()
        errBuf = Data()
        last = nil
        rows = []
        banner = nil
        running = true
        summary = "Tracing \(t)…"
    }

    // stop ends the running ttrace quietly.
    func stop() {
        guard let p = proc else { return }
        runId += 1 // ignore whatever the old process still sends
        if p.isRunning { p.terminate() }
        proc = nil
        running = false
    }

    func stopByUser() {
        stop()
        summary = last.map { Tracer.summaryText($0) + " · stopped" } ?? ""
    }

    private func gotOut(_ id: Int, _ d: Data) {
        guard id == runId, !d.isEmpty else { return }
        outBuf.append(d)
        while let nl = outBuf.firstIndex(of: 0x0A) {
            let line = outBuf.subdata(in: outBuf.startIndex..<nl)
            outBuf.removeSubrange(outBuf.startIndex...nl)
            gotLine(line)
        }
    }

    private func gotErr(_ id: Int, _ d: Data) {
        guard id == runId, !d.isEmpty else { return }
        errBuf.append(d)
        while let nl = errBuf.firstIndex(of: 0x0A) {
            let line = String(decoding: errBuf.subdata(in: errBuf.startIndex..<nl), as: UTF8.self)
            errBuf.removeSubrange(errBuf.startIndex...nl)
            let l = line.trimmingCharacters(in: .whitespaces)
            if l.isEmpty { continue }
            errLines.append(l.hasPrefix("ttrace: ") ? String(l.dropFirst(8)) : l)
        }
    }

    private func gotLine(_ line: Data) {
        guard let s = try? JSONDecoder().decode(Snapshot.self, from: line) else { return } // not a result line
        last = s
        if selfTest != nil { SelfTest.log("round \(s.round), hops \(s.hops.count), geo: \(s.geo)") }
        let new = s.hops.map(HopRow.init)
        if new != rows { rows = new }
        summary = Tracer.summaryText(s)

        if selfTest != nil {
            let places = s.hops.filter { $0.place != nil }.count
            if (places > 0 && s.round >= 3) || s.round >= 10 {
                selfTestDone("ok hops=\(s.hops.count) places=\(places) round=\(s.round)")
            }
        }
    }

    private func exited(_ id: Int, code: Int32, signaled: Bool) {
        guard id == runId else { return }
        if !errBuf.isEmpty { gotErr(id, Data([0x0A])) }
        proc = nil
        running = false
        if code != 0 || signaled {
            if selfTest != nil { SelfTest.log("ttrace ended with \(code): \(errLines)") }
            let msg = errLines.isEmpty ? "ttrace stopped unexpectedly (exit code \(code))." : errLines.joined(separator: " ")
            fail("The trace could not run", msg + " Check the address and the internet connection, then try again.")
        } else {
            banner = Banner(kind: .info, title: "The trace has ended", message: "Press Start to run it again.",
                            action: "Start again", run: { [weak self] in self?.start() })
        }
        if let l = last { summary = Tracer.summaryText(l) + " · ended" }
    }

    static func summaryText(_ s: Snapshot) -> String {
        var t = s.target
        if !s.ip.isEmpty && s.ip != s.target { t += " (\(s.ip))" }
        if !s.family.isEmpty { t += " · " + s.family }
        if !s.probe.isEmpty { t += " · " + s.probe }
        t += " · round \(s.round)"
        t += s.reached ? " · target reached" : " · target not reached yet"
        return t
    }

    private func fail(_ title: String, _ message: String) {
        banner = Banner(kind: .error, title: title, message: message, action: "Try again", run: { [weak self] in self?.start() })
        if selfTest != nil { selfTestDone("error: \(title): \(message)") }
    }

    private func selfTestDone(_ result: String) {
        guard let t = selfTest, !selfTestFinished else { return }
        selfTestFinished = true
        try? (result + "\n").write(toFile: t.resultFile, atomically: true, encoding: .utf8)
        // let the window draw the last state, then keep a picture of it
        DispatchQueue.main.asyncAfter(deadline: .now() + 1.5) {
            if let v = NSApp.windows.first(where: { $0.isVisible })?.contentView,
               let rep = v.bitmapImageRepForCachingDisplay(in: v.bounds) {
                v.cacheDisplay(in: v.bounds, to: rep)
                try? rep.representation(using: .png, properties: [:])?.write(to: URL(fileURLWithPath: t.resultFile + ".png"))
            }
            self.stop()
            NSApp.terminate(nil)
        }
    }
}
