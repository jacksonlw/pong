import AppKit
import SwiftUI

/// The line above the question: the Project's logo and colour, its name and the Session's title,
/// and the context ring at the right when the Harness reports figures. The Session is named by
/// its title only; its id shows on hover, and a click opens it in its Harness.
public struct ProjectLine: View {
    var ping: Ping
    @Environment(\.cardContext) private var ctx
    @State private var hover = false

    public init(ping: Ping) {
        self.ping = ping
    }

    public var body: some View {
        let card = ctx.directory.card(for: ping)
        HStack(spacing: 8) {
            if let project = card.project {
                ProjectTile(project: project, logo: card.logo)
            }
            Text(text(card))
                .brandStyle(.secondary)
                .foregroundStyle(Brand.secondary)
                .lineLimit(1)
                .truncationMode(.tail)
                // Steps up on hover like the question, its block reaching just past the text.
                .padding(.horizontal, 6).padding(.vertical, 3)
                .background(hover ? Brand.row : .clear, in: Rectangle())
                .hoverHighlight($hover)
                .padding(.horizontal, -6).padding(.vertical, -3)
                .help(ping.session.isEmpty ? "" : "Session \(ping.session)")
                .onTapGesture { SessionOpening.open?(ping) }
                .accessibilityAddTraits(.isButton)
                .accessibilityHint("Opens the Session in its Harness")
            if let used = card.session?.context?.usedFraction {
                Spacer(minLength: 0)
                ContextRing(used: used).contextHover(.ring, ping: ping)
            }
        }
    }

    private func text(_ card: Directory.Card) -> String {
        let session = card.session?.title ?? ""
        let folder = ping.folder.map { URL(fileURLWithPath: $0).lastPathComponent } ?? ""
        var place = card.project?.name ?? folder
        // The computer is named only once the set has more than one (the Engine says so by
        // sending it), and its reachability with it.
        if let c = ping.computer {
            let on = c.reachable ? c.name : "\(c.name), unreachable"
            place = place.isEmpty ? on : "\(place) on \(on)"
        }
        if place.isEmpty { return session.isEmpty ? "Session" : session }
        return session.isEmpty ? place : "\(place) · \(session)"
    }
}

/// The Project's colour, with its logo inside when it has one, else its initial.
struct ProjectTile: View {
    var project: Project
    var logo: Data?
    var side: CGFloat = 22

    var body: some View {
        let fill = Color(hexString: project.colour) ?? .gray
        Rectangle()
            .fill(fill)
            .frame(width: side, height: side)
            .overlay {
                if let logo, let image = NSImage(data: logo) {
                    Image(nsImage: image).resizable().scaledToFit().padding(side * 3 / 22)
                } else {
                    Text(project.name.prefix(1).uppercased())
                        .brandStyle(.label)
                        .foregroundStyle(Self.readableInk(on: project.colour))
                }
            }
    }

    /// The kit's fixed Ink or White, whichever contrasts more with the tile.
    static func readableInk(on hex: String) -> Color {
        guard let v = UInt32(hex.dropFirst(), radix: 16) else { return Brand.fixedInk }
        let tile = luminance(v)
        let onWhite = (luminance(Brand.fixedWhite) + 0.05) / (tile + 0.05)
        let onInk = (tile + 0.05) / (luminance(Brand.fixedInk) + 0.05)
        return onWhite >= onInk ? Brand.fixedWhite : Brand.fixedInk
    }

    static func luminance(_ c: Color) -> Double {
        guard let s = NSColor(c).usingColorSpace(.sRGB) else { return 0 }
        let v = UInt32(s.redComponent * 255) << 16 | UInt32(s.greenComponent * 255) << 8 | UInt32(s.blueComponent * 255)
        return luminance(v)
    }

    static func luminance(_ v: UInt32) -> Double {
        func lin(_ c: UInt32) -> Double {
            let s = Double(c & 0xff) / 255
            return s <= 0.04045 ? s / 12.92 : pow((s + 0.055) / 1.055, 2.4)
        }
        return 0.2126 * lin(v >> 16) + 0.7152 * lin(v >> 8) + 0.0722 * lin(v)
    }
}

/// A circle whose border fills as the Session's context window fills; the warning colour from 80%.
struct ContextRing: View {
    var used: Double

    static func isFull(_ used: Double) -> Bool { ContextFigures.isFull(used) }

    var body: some View {
        ZStack {
            Circle().stroke(Brand.track, lineWidth: 2.5)
            Circle()
                .trim(from: 0, to: min(max(used, 0), 1))
                .stroke(Self.isFull(used) ? Brand.warning : Brand.secondary, style: StrokeStyle(lineWidth: 2.5, lineCap: .round))
                .rotationEffect(.degrees(-90))
        }
        .frame(width: 14, height: 14)
        .frame(width: 26, height: 22)
        .contentShape(Rectangle())
        .accessibilityLabel("Context \(Int((used * 100).rounded()))% full")
    }
}

extension Color {
    /// A Project's own colour (content, not brand), e.g. `#c4ecd4`.
    init?(hexString: String) {
        guard hexString.hasPrefix("#"), hexString.count == 7, let v = UInt32(hexString.dropFirst(), radix: 16) else { return nil }
        self.init(hex: v)
    }

    init(hex: UInt32) {
        self.init(.sRGB, red: Double(hex >> 16 & 0xff) / 255, green: Double(hex >> 8 & 0xff) / 255, blue: Double(hex & 0xff) / 255)
    }
}
