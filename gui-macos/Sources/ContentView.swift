import SwiftUI

struct ContentView: View {
    @ObservedObject var t: Tracer
    @FocusState private var targetFocused: Bool

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            // what to trace
            HStack(alignment: .bottom, spacing: 10) {
                VStack(alignment: .leading, spacing: 4) {
                    Text("Host name or IP address to trace")
                        .font(.callout).foregroundStyle(.secondary)
                    TextField("for example example.com or 1.1.1.1", text: $t.target)
                        .textFieldStyle(.roundedBorder)
                        .focused($targetFocused)
                        .onSubmit { t.start() }
                }
                VStack(alignment: .leading, spacing: 4) {
                    Text("IP version").font(.callout).foregroundStyle(.secondary)
                    Picker("IP version", selection: $t.family) {
                        Text("Automatic").tag(0)
                        Text("IPv4 only").tag(1)
                        Text("IPv6 only").tag(2)
                    }
                    .labelsHidden()
                    .frame(width: 150)
                    .onChange(of: t.family) { _ in
                        // a running trace switches to the chosen IP version right away
                        if t.running { t.start() }
                    }
                }
                Button {
                    if t.running {
                        t.stopByUser()
                    } else {
                        t.start()
                        if t.target.trimmingCharacters(in: .whitespaces).isEmpty { targetFocused = true }
                    }
                } label: {
                    Text(t.running ? "Stop" : "Start").frame(minWidth: 70)
                }
                .buttonStyle(.borderedProminent)
                .controlSize(.large)
            }

            if let b = t.banner {
                BannerView(banner: b) { t.banner = nil }
            }

            if !t.summary.isEmpty {
                Text(t.summary).font(.headline).textSelection(.enabled)
            }
            if let path = t.last?.asPath, !path.isEmpty {
                Text("Networks on the way: " + path.map(\.label).joined(separator: " → "))
                    .font(.callout).foregroundStyle(.secondary)
                    .textSelection(.enabled)
                    .help("The networks (autonomous systems) the route passes through, in order")
            }

            VSplitView {
                HopTable(t: t)
                    .frame(minHeight: 160)
                MapView(snapshot: t.last, running: t.running) { t.start() }
                    .frame(minHeight: 180)
                    .background(RoundedRectangle(cornerRadius: 8).fill(Color(nsColor: .controlBackgroundColor)))
                    .clipShape(RoundedRectangle(cornerRadius: 8))
                    .overlay(RoundedRectangle(cornerRadius: 8).stroke(Color(nsColor: .separatorColor)))
                    .padding(.top, 8)
            }
        }
        .padding(16)
        .frame(minWidth: 900, minHeight: 600)
        .onAppear { targetFocused = true }
    }
}

struct BannerView: View {
    let banner: Banner
    let close: () -> Void

    var body: some View {
        let isError = banner.kind == .error
        HStack(alignment: .top, spacing: 10) {
            Image(systemName: isError ? "exclamationmark.triangle.fill" : "info.circle.fill")
                .foregroundStyle(isError ? Color.red : Color.accentColor)
                .font(.title3)
            VStack(alignment: .leading, spacing: 2) {
                Text(banner.title).font(.headline)
                Text(banner.message).textSelection(.enabled).fixedSize(horizontal: false, vertical: true)
            }
            Spacer(minLength: 8)
            if let a = banner.action, let run = banner.run {
                Button(a) { run() }
            }
            Button("Close") { close() }
        }
        .padding(10)
        .background(RoundedRectangle(cornerRadius: 8).fill((isError ? Color.red : Color.accentColor).opacity(0.12)))
    }
}

struct HopTable: View {
    @ObservedObject var t: Tracer

    var body: some View {
        Table(t.rows) {
            TableColumn("#") { r in
                Text(r.ttl).foregroundStyle(.secondary).monospacedDigit()
                    .frame(maxWidth: .infinity, alignment: .trailing)
            }
            .width(32)
            TableColumn("Router") { r in
                Text(r.host).textSelection(.enabled).lineLimit(1).truncationMode(.middle)
            }
            .width(min: 160, ideal: 230)
            TableColumn("Network") { r in
                Text(r.network).lineLimit(1).help(r.networkFull)
            }
            .width(min: 120, ideal: 190)
            TableColumn("CC") { r in
                Text(r.country).help("Country where the network is registered")
            }
            .width(32)
            TableColumn("Place (approx.)") { r in
                Text(r.place).lineLimit(1).help(r.place)
            }
            .width(min: 80, ideal: 120)
            TableColumn("Loss") { r in
                Text(r.loss).monospacedDigit().frame(maxWidth: .infinity, alignment: .trailing)
                    .help("Share of probes without an answer")
            }
            .width(60)
            TableColumn("Sent") { r in
                Text(r.sent).monospacedDigit().frame(maxWidth: .infinity, alignment: .trailing)
            }
            .width(40)
            TableColumn("Last ms") { r in
                Text(r.last).monospacedDigit().frame(maxWidth: .infinity, alignment: .trailing)
            }
            .width(56)
            TableColumn("Avg ms") { r in
                HStack(spacing: 4) {
                    if !r.jump.isEmpty {
                        Text(r.jump).font(.caption).foregroundStyle(.orange)
                            .help("Latency rises here and stays higher on the following hops")
                    }
                    Text(r.avg).monospacedDigit()
                }
                .frame(maxWidth: .infinity, alignment: .trailing)
            }
            .width(min: 56, ideal: 90)
            TableColumn("Best – worst ms") { r in
                Text(r.bestWorst).monospacedDigit().frame(maxWidth: .infinity, alignment: .trailing)
            }
            .width(min: 90, ideal: 100)
        }
        .overlay {
            if t.rows.isEmpty {
                Text(t.running
                     ? "Sending the first probes… the routers on the way appear here in a moment."
                     : "Type an address above and press Start. Every router on the way to it will be listed here.")
                    .foregroundStyle(.secondary)
                    .multilineTextAlignment(.center)
                    .padding(24)
            }
        }
    }
}
