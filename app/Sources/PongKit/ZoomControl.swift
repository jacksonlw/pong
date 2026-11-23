import SwiftUI

/// Pong's zoom, shown above the Bar as it changes: zoom out, the zoom now (a click lists every
/// zoom to choose from) and zoom in, drawn like an image's zoom controls.
public struct ZoomControl: View {
    var scale: CGFloat
    var steps: [CGFloat]
    var choose: (CGFloat) -> Void
    var hovering: (Bool) -> Void
    @State private var hover = false

    /// `hovering` is told when the pointer comes onto the controls and leaves them.
    public init(scale: CGFloat, steps: [CGFloat], choose: @escaping (CGFloat) -> Void, hovering: @escaping (Bool) -> Void = { _ in }) {
        self.scale = scale
        self.steps = steps
        self.choose = choose
        self.hovering = hovering
    }

    static func percent(_ s: CGFloat) -> String { "\(Int((s * 100).rounded()))%" }

    private var smaller: CGFloat? { steps.last { $0 < scale - 0.001 } }
    private var larger: CGFloat? { steps.first { $0 > scale + 0.001 } }

    public var body: some View {
        HStack(spacing: 0) {
            IconButton(symbol: "minus.magnifyingglass", label: "Zoom out", help: "Zoom out (⌘−)", enabled: smaller != nil,
                       fill: Brand.surface) { if let s = smaller { choose(s) } }
            Menu {
                ForEach(steps, id: \.self) { s in
                    Button { choose(s) } label: {
                        if abs(s - scale) < 0.001 { Label(Self.percent(s), systemImage: "checkmark") } else { Text(Self.percent(s)) }
                    }
                }
            } label: {
                Text(Self.percent(scale))
                    .brandStyle(.label)
                    .monospacedDigit()
                    .foregroundStyle(hover ? Brand.text : Brand.secondary)
                    .frame(minWidth: 52, minHeight: 30)
                    .background(hover ? Brand.row : Brand.surface, in: Rectangle())
                    .contentShape(Rectangle())
            }
            .menuStyle(.button)
            .buttonStyle(FocusedPlain())
            .menuIndicator(.hidden)
            .fixedSize()
            .hoverHighlight($hover)
            .help("Choose a zoom (⌘0 for 100%)")
            .accessibilityLabel("Zoomed to \(Self.percent(scale)), choose a zoom")
            IconButton(symbol: "plus.magnifyingglass", label: "Zoom in", help: "Zoom in (⌘+)", enabled: larger != nil,
                       fill: Brand.surface) { if let s = larger { choose(s) } }
        }
        .background(Brand.surface)
        .background(HoverTracker(onChange: hovering))
    }
}
