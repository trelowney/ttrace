import Foundation

// One line of "ttrace --json-stream": the state after a round.
struct Snapshot: Decodable {
    var target = ""
    var ip = ""
    var family = ""
    var probe = ""
    var round = 0
    var reached = false
    var hops: [Hop] = []
    var geo = ""
    var asPath: [PathStep] = []

    enum CodingKeys: String, CodingKey {
        case target, ip, family, probe, round, reached, hops, geo
        case asPath = "as_path"
    }

    init(from d: Decoder) throws {
        let c = try d.container(keyedBy: CodingKeys.self)
        target = c.get(.target, "")
        ip = c.get(.ip, "")
        family = c.get(.family, "")
        probe = c.get(.probe, "")
        round = c.get(.round, 0)
        reached = c.get(.reached, false)
        hops = c.get(.hops, [])
        geo = c.get(.geo, "")
        asPath = c.get(.asPath, [])
    }
}

struct Hop: Decodable {
    var ttl = 0
    var addr = ""
    var host = ""
    var asn = ""
    var asName = ""
    var country = ""
    var isPrivate = false
    var hidden = false
    var sent = 0
    var recv = 0
    var loss = 0.0
    var last = 0.0
    var avg = 0.0
    var best = 0.0
    var worst = 0.0
    var place: Place?
    var ix = ""
    var jump = 0.0

    enum CodingKeys: String, CodingKey {
        case ttl, addr, host, asn, country, hidden, sent, recv, place, ix
        case asName = "as_name"
        case isPrivate = "private"
        case loss = "loss_pct"
        case last = "last_ms"
        case avg = "avg_ms"
        case best = "best_ms"
        case worst = "worst_ms"
        case jump = "jump_ms"
    }

    init(from d: Decoder) throws {
        let c = try d.container(keyedBy: CodingKeys.self)
        ttl = c.get(.ttl, 0)
        addr = c.get(.addr, "")
        host = c.get(.host, "")
        asn = c.get(.asn, "")
        asName = c.get(.asName, "")
        country = c.get(.country, "")
        isPrivate = c.get(.isPrivate, false)
        hidden = c.get(.hidden, false)
        sent = c.get(.sent, 0)
        recv = c.get(.recv, 0)
        loss = c.get(.loss, 0.0)
        last = c.get(.last, 0.0)
        avg = c.get(.avg, 0.0)
        best = c.get(.best, 0.0)
        worst = c.get(.worst, 0.0)
        place = try? c.decodeIfPresent(Place.self, forKey: .place)
        ix = c.get(.ix, "")
        jump = c.get(.jump, 0.0)
    }
}

struct Place: Decodable, Equatable {
    var city = ""
    var region = ""
    var country = ""
    var lat = 0.0
    var lon = 0.0

    enum CodingKeys: String, CodingKey { case city, region, country, lat, lon }

    init(from d: Decoder) throws {
        let c = try d.container(keyedBy: CodingKeys.self)
        city = c.get(.city, "")
        region = c.get(.region, "")
        country = c.get(.country, "")
        lat = c.get(.lat, 0.0)
        lon = c.get(.lon, 0.0)
    }

    var name: String {
        if !city.isEmpty && !country.isEmpty { return "\(city), \(country)" }
        return city.isEmpty ? country : city
    }
}

// One network on the route: an AS, or an internet exchange (ix set).
struct PathStep: Decodable {
    var asn = ""
    var name = ""
    var ix = ""

    enum CodingKeys: String, CodingKey { case asn, name, ix }

    init(from d: Decoder) throws {
        let c = try d.container(keyedBy: CodingKeys.self)
        asn = c.get(.asn, "")
        name = c.get(.name, "")
        ix = c.get(.ix, "")
    }

    var label: String {
        if !ix.isEmpty { return ix }
        return name.isEmpty ? "AS" + asn : "AS\(asn) \(shortAS(name))"
    }
}

// shortAS keeps the handle of an AS name: "CLOUDFLARENET - Cloudflare, Inc." → "CLOUDFLARENET".
func shortAS(_ name: String) -> String {
    if let r = name.range(of: " - "), r.lowerBound > name.startIndex { return String(name[..<r.lowerBound]) }
    return name
}

extension KeyedDecodingContainer {
    // get reads a value, or gives def when it is missing or of another type,
    // so a newer ttrace with more or changed fields never breaks the window.
    func get<T: Decodable>(_ key: Key, _ def: T) -> T {
        (try? decodeIfPresent(T.self, forKey: key)) ?? def
    }
}

// HopRow is one line of the table, already as text.
struct HopRow: Identifiable, Equatable {
    let id: Int
    var ttl = ""
    var host = ""
    var network = ""
    var networkFull = "" // for the hover text
    var country = ""
    var place = ""
    var loss = ""
    var sent = ""
    var last = ""
    var avg = ""
    var jump = ""
    var bestWorst = ""

    init(_ h: Hop) {
        id = h.ttl
        ttl = String(h.ttl)
        if h.hidden {
            host = "(hidden)"
        } else if h.addr.isEmpty {
            host = "no answer"
        } else {
            host = h.host.isEmpty ? h.addr : "\(h.host)  (\(h.addr))"
        }
        if h.isPrivate {
            network = "private network"
        } else {
            // like the command line: "AS13335 CLOUDFLARENET", on an internet
            // exchange "AS13335 Frys-IX (CLOUDFLARENET)"
            func label(_ name: String) -> String {
                let l: String
                if h.ix.isEmpty { l = name } else if name.isEmpty { l = h.ix } else { l = "\(h.ix) (\(name))" }
                return h.asn.isEmpty ? l : ("AS" + h.asn + (l.isEmpty ? "" : " " + l))
            }
            network = label(shortAS(h.asName))
            networkFull = label(h.asName)
            if !h.ix.isEmpty { networkFull += "\n\(h.ix) is an internet exchange; the AS is the network using this address there" }
        }
        country = h.country
        place = h.place?.name ?? ""
        loss = h.sent > 0 ? String(format: "%.1f %%", h.loss) : ""
        sent = String(h.sent)
        let got = h.recv > 0
        last = got ? String(format: "%.1f", h.last) : ""
        avg = got ? String(format: "%.1f", h.avg) : ""
        jump = h.jump > 0 ? String(format: "▲ +%.1f", h.jump) : ""
        bestWorst = got ? String(format: "%.1f – %.1f", h.best, h.worst) : ""
    }
}
