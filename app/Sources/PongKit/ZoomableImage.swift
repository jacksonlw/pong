import AppKit
import SwiftUI

/// How far an image is zoomed: nil fits it to the room it has; otherwise its scale, where 1 is
/// one image point per screen point.
@MainActor @Observable
final class ImageZoom {
    var scale: CGFloat?
    /// The scale that fits the image, kept by the view as its room changes.
    @ObservationIgnored var fit: CGFloat = 1

    static let steps: [CGFloat] = [0.1, 0.25, 0.5, 0.75, 1, 1.5, 2, 3, 4, 6, 8]
    static var range: ClosedRange<CGFloat> { steps.first!...steps.last! }

    var shown: CGFloat { scale ?? fit }
    var percent: Int { Int((shown * 100).rounded()) }

    func zoomIn() { scale = Self.steps.first { $0 > shown + 0.001 } ?? shown }
    func zoomOut() { scale = Self.steps.last { $0 < shown - 0.001 } ?? shown }
    func toFit() { scale = nil }
    func set(_ s: CGFloat) { scale = min(max(s, Self.range.lowerBound), Self.range.upperBound) }
}

/// ⌘+, ⌘− and ⌘0 (or ⌃) zoom the image under the pointer, or the one shown full screen, instead
/// of the whole of Pong.
@MainActor
public enum ImageZoomKeys {
    /// The image the keys act on now.
    static weak var target: ImageZoom?
    /// The full-screen image, which takes the keys wherever the pointer is.
    static weak var shownFullScreen: ImageZoom?

    /// Zooms the image the keys are for; false leaves the keys to Pong's own zoom.
    public static func handle(_ e: NSEvent) -> Bool {
        guard let z = shownFullScreen ?? target, let key = ZoomKey(e) else { return false }
        switch key {
        case .in: z.zoomIn()
        case .out: z.zoomOut()
        case .reset: z.toFit()
        }
        return true
    }
}

/// A zoom key: ⌘ or ⌃ with +, =, − or 0.
public enum ZoomKey {
    case `in`, out, reset

    public init?(_ e: NSEvent) {
        let keys = e.modifierFlags.intersection([.command, .control, .option])
        guard keys == .command || keys == .control else { return nil }
        switch e.charactersIgnoringModifiers {
        case "+", "=": self = .in
        case "-", "_": self = .out
        case "0": self = .reset
        default: return nil
        }
    }
}

/// An image that fits its room until zoomed, then scrolls both ways. The zoom controls sit in its
/// corner; a pinch zooms too, and so do ⌘+ and ⌘− while the pointer is over it.
struct ZoomableImage: View {
    var image: NSImage
    var title: String
    /// Fitting never draws the image larger than it is.
    var growsToFit = false
    /// The controls' colours, for an image on the ink of the full-screen view.
    var onInk = false
    /// A click on the image itself; nil leaves clicks alone.
    var click: (() -> Void)?
    var fullScreen = false
    @State private var zoom = ImageZoom()
    @State private var pinchFrom: CGFloat?

    var body: some View {
        GeometryReader { room in
            let fit = fitScale(in: room.size)
            let scale = zoom.scale ?? fit
            let size = CGSize(width: image.size.width * scale, height: image.size.height * scale)
            ScrollView([.horizontal, .vertical]) {
                Image(nsImage: image)
                    .resizable()
                    .interpolation(scale > 2 ? .none : .high)
                    .frame(width: size.width, height: size.height)
                    .contentShape(Rectangle())
                    .onTapGesture { click?() }
                    .accessibilityLabel(title)
                    .frame(minWidth: room.size.width, minHeight: room.size.height, alignment: fullScreen ? .center : .top)
            }
            .scrollIndicators(zoom.scale == nil ? .never : .automatic)
            .onChange(of: fit, initial: true) { zoom.fit = fit }
        }
        .gesture(MagnifyGesture()
            .onChanged { v in
                let from = pinchFrom ?? zoom.shown
                pinchFrom = from
                zoom.set(from * v.magnification)
            }
            .onEnded { _ in pinchFrom = nil })
        .overlay(alignment: .bottomTrailing) { controls.padding(8) }
        .onContinuousHover { phase in
            switch phase {
            case .active: ImageZoomKeys.target = zoom
            case .ended: if ImageZoomKeys.target === zoom { ImageZoomKeys.target = nil }
            }
        }
        .onAppear { if fullScreen { ImageZoomKeys.shownFullScreen = zoom } }
        .onDisappear {
            if ImageZoomKeys.shownFullScreen === zoom { ImageZoomKeys.shownFullScreen = nil }
            if ImageZoomKeys.target === zoom { ImageZoomKeys.target = nil }
        }
    }

    private func fitScale(in room: CGSize) -> CGFloat {
        guard image.size.width > 0, image.size.height > 0, room.width > 0, room.height > 0 else { return 1 }
        let s = min(room.width / image.size.width, room.height / image.size.height)
        return growsToFit ? s : min(s, 1)
    }

    private var controls: some View {
        let colour: Color? = onInk ? Brand.fixedWhite : nil
        return HStack(spacing: 0) {
            IconButton(symbol: "minus.magnifyingglass", label: "Zoom out", help: "Zoom out (⌘−)", colour: colour,
                       fill: onInk ? .clear : Brand.surface) { zoom.zoomOut() }
            HoverButton(action: { zoom.toFit() }) { hover in
                Text(zoom.scale == nil ? "Fit" : "\(zoom.percent)%")
                    .brandStyle(.label)
                    .monospacedDigit()
                    .foregroundStyle(onInk ? Brand.fixedWhite : hover ? Brand.text : Brand.secondary)
                    .frame(minWidth: 52, minHeight: 30)
                    .background(onInk ? Color.clear : hover ? Brand.row : Brand.surface, in: Rectangle())
                    .contentShape(Rectangle())
            }
            .help("Fit to the window (⌘0)")
            .accessibilityLabel(zoom.scale == nil ? "Fitted" : "Zoomed to \(zoom.percent)%, fit to the window")
            IconButton(symbol: "plus.magnifyingglass", label: "Zoom in", help: "Zoom in (⌘+)", colour: colour,
                       fill: onInk ? .clear : Brand.surface) { zoom.zoomIn() }
        }
    }
}
