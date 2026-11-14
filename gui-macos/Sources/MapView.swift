import SwiftUI

// Land is the world outline, loaded once from land.bin in the app:
// int32 number of rings, then per ring int32 number of points and the
// points as int16 longitude and latitude in hundredths of a degree
// (little-endian). Three copies side by side, so routes across the Pacific
// have land on both sides.
enum Land {
    static let loaded: (path: Path?, error: String) = load()

    private static func load() -> (Path?, String) {
        let fail = "The world outline could not be loaded, so only the route is drawn. Downloading the app again may help."
        guard let url = Bundle.main.url(forResource: "land", withExtension: "bin"),
              let data = try? Data(contentsOf: url) else { return (nil, fail) }
        let b = [UInt8](data)
        var pos = 0
        func i32() -> Int? {
            guard pos + 4 <= b.count else { return nil }
            let v = UInt32(b[pos]) | UInt32(b[pos + 1]) << 8 | UInt32(b[pos + 2]) << 16 | UInt32(b[pos + 3]) << 24
            pos += 4
            return Int(Int32(bitPattern: v))
        }
        func i16() -> Double? {
            guard pos + 2 <= b.count else { return nil }
            let v = UInt16(b[pos]) | UInt16(b[pos + 1]) << 8
            pos += 2
            return Double(Int16(bitPattern: v)) / 100
        }
        guard let rings = i32(), rings > 0 else { return (nil, fail) }
        var path = Path()
        for _ in 0..<rings {
            guard let n = i32(), n >= 0 else { return (nil, fail) }
            var pts: [CGPoint] = []
            pts.reserveCapacity(n)
            for _ in 0..<n {
                guard let lon = i16(), let lat = i16() else { return (nil, fail) }
                pts.append(CGPoint(x: lon, y: lat))
            }
            if pts.count < 3 { continue }
            for shift in [-360.0, 0, 360] {
                path.addLines(pts.map { CGPoint(x: $0.x + shift, y: $0.y) })
                path.closeSubpath()
            }
        }
        return (path, "")
    }
}

// A place on the map: consecutive hops at the same spot share one marker.
private struct Group {
    var first: Int
    var lastTtl: Int
    let place: Place
    let host: String
    let lon: Double // unwrapped, so a route over the Pacific stays connected
}

private struct Marker: Identifiable {
    let id: Int
    let point: CGPoint
    let label: String
    let tip: String
    let isTarget: Bool
}

struct MapView: View {
    let snapshot: Snapshot?
    let running: Bool
    let retry: () -> Void

    var body: some View {
        GeometryReader { geo in
            let markers = self.markers(in: geo.size)
            ZStack(alignment: .topLeading) {
                Canvas { ctx, size in
                    if let land = Land.loaded.path {
                        ctx.fill(land.applying(viewTransform(size)), with: .color(Color.primary.opacity(0.13)))
                    }
                    if markers.count > 1 {
                        var line = Path()
                        line.addLines(markers.map(\.point))
                        ctx.stroke(line, with: .color(.accentColor),
                                   style: StrokeStyle(lineWidth: 2.5, lineCap: .round, lineJoin: .round))
                    }
                    for m in markers {
                        let r = CGRect(x: m.point.x - 6, y: m.point.y - 6, width: 12, height: 12)
                        ctx.fill(Path(ellipseIn: r), with: .color(m.isTarget ? .green : .accentColor))
                        ctx.draw(Text(m.label).font(.caption.weight(.semibold)),
                                 at: CGPoint(x: m.point.x + 9, y: m.point.y), anchor: .leading)
                    }
                }
                // invisible spots over the markers, for the hover text
                ForEach(markers) { m in
                    Color.clear
                        .frame(width: 22, height: 22)
                        .contentShape(Circle())
                        .help(m.tip)
                        .position(m.point)
                }

                // note in the corner: always says what is going on and what to do
                let note = self.note(hasPlaces: !markers.isEmpty)
                if !note.text.isEmpty {
                    VStack(alignment: .leading, spacing: 8) {
                        Text(note.text).fixedSize(horizontal: false, vertical: true)
                        if note.retry { Button("Try again", action: retry) }
                    }
                    .padding(.horizontal, 12).padding(.vertical, 8)
                    .frame(maxWidth: 560, alignment: .leading)
                    .background(RoundedRectangle(cornerRadius: 6).fill(Color(nsColor: .windowBackgroundColor)))
                    .padding(12)
                }

                Text("Places are approximate · IP locations: DB-IP.com (CC BY 4.0) · Map: Natural Earth")
                    .font(.caption).foregroundStyle(.secondary)
                    .padding(.horizontal, 12).padding(.vertical, 8)
                    .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .bottomTrailing)
            }
        }
    }

    private func groups() -> [Group] {
        var out: [Group] = []
        guard let s = snapshot else { return out }
        for h in s.hops {
            guard let p = h.place else { continue }
            if var g = out.last, g.place.lat == p.lat, g.place.lon == p.lon {
                g.lastTtl = h.ttl
                out[out.count - 1] = g
                continue
            }
            var lon = p.lon
            if let prev = out.last?.lon {
                while lon - prev > 180 { lon -= 360 }
                while lon - prev < -180 { lon += 360 }
            }
            out.append(Group(first: h.ttl, lastTtl: h.ttl, place: p, host: h.host.isEmpty ? h.addr : h.host, lon: lon))
        }
        return out
    }

    // view: all places with some margin, or the whole world
    private struct MapFrame {
        var k = 1.0, cx = 0.0, cy = 0.0, scale = 1.0
    }

    private func mapFrame(_ size: CGSize) -> MapFrame {
        let gs = groups()
        var minLat = 90.0, maxLat = -90.0, minLon = 1e9, maxLon = -1e9
        for g in gs {
            minLat = min(minLat, g.place.lat); maxLat = max(maxLat, g.place.lat)
            minLon = min(minLon, g.lon); maxLon = max(maxLon, g.lon)
        }
        if gs.isEmpty { minLat = -58; maxLat = 78; minLon = -170; maxLon = 190 }
        let midLat = (minLat + maxLat) / 2
        let k = max(cos(midLat * .pi / 180), 0.2) // shrink east-west away from the equator
        let spanX = max((maxLon - minLon) * k * 1.25, 16)
        let spanY = max((maxLat - minLat) * 1.25, 9)
        let scale = min(Double(size.width) / spanX, Double(size.height) / spanY) // points per degree
        return MapFrame(k: k, cx: (minLon + maxLon) / 2 * k, cy: midLat, scale: scale)
    }

    private func viewTransform(_ size: CGSize) -> CGAffineTransform {
        let v = mapFrame(size)
        let w = Double(size.width), h = Double(size.height)
        return CGAffineTransform(a: v.k * v.scale, b: 0, c: 0, d: -v.scale,
                                 tx: -v.cx * v.scale + w / 2, ty: v.cy * v.scale + h / 2)
    }

    private func markers(in size: CGSize) -> [Marker] {
        guard size.width > 10, size.height > 10, let s = snapshot else { return [] }
        let v = mapFrame(size)
        let w = Double(size.width), h = Double(size.height)
        let lastTtl = s.hops.last?.ttl ?? -1
        return groups().map { g in
            let pt = CGPoint(x: (g.lon * v.k - v.cx) * v.scale + w / 2, y: (v.cy - g.place.lat) * v.scale + h / 2)
            let isTarget = s.reached && g.lastTtl == lastTtl
            let hops = g.first == g.lastTtl ? "\(g.first)" : "\(g.first)–\(g.lastTtl)"
            var tip = "\(g.first == g.lastTtl ? "Hop" : "Hops") \(hops): \(g.place.name) (approximate)\n\(g.host)"
            if isTarget { tip += "\nThis is the target." }
            return Marker(id: g.first, point: pt, label: hops, tip: tip, isTarget: isTarget)
        }
    }

    private func note(hasPlaces: Bool) -> (text: String, retry: Bool) {
        if !Land.loaded.error.isEmpty { return (Land.loaded.error, false) }
        guard let s = snapshot else {
            return (running
                ? "The route is drawn here as soon as the first answers arrive."
                : "The route will be drawn here on a world map once a trace runs.", false)
        }
        if s.geo.hasPrefix("The location data for the map could not be downloaded") {
            return (s.geo + (hasPlaces
                ? " The older data is used meanwhile."
                : " The table works without it. Press Try again to download it again."), true)
        }
        if !s.geo.isEmpty {
            return (s.geo + " — this happens once. The map fills in by itself when it is done; the table already works.", false)
        }
        if !hasPlaces {
            return ("No router on this route has a known place yet. Private addresses (like your home network) " +
                    "and routers that do not answer cannot be placed on the map.", false)
        }
        return ("", false)
    }
}
