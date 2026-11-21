import AppKit
import SwiftUI

/// How large Pong draws everything in its windows, as a browser zooms a page: ⌘+ and ⌘− (or
/// ⌃+ and ⌃−) step it, ⌘0 puts it back. Kept between launches.
@MainActor
final class Zoom {
    static let shared = Zoom()
    static let changed = Notification.Name("PongZoomChanged")
    private static let key = "zoom"
    static let steps: [CGFloat] = [0.75, 0.85, 1, 1.1, 1.25, 1.5, 1.75, 2]

    private(set) var scale: CGFloat

    private init() {
        let kept = UserDefaults.standard.double(forKey: Self.key)
        scale = Self.steps.contains(kept) ? kept : 1
    }

    func zoomIn() { set(Self.steps.first { $0 > scale + 0.001 } ?? scale) }
    func zoomOut() { set(Self.steps.last { $0 < scale - 0.001 } ?? scale) }
    func reset() { set(1) }

    private func set(_ s: CGFloat) {
        guard s != scale else { return }
        let was = scale
        scale = s
        UserDefaults.standard.set(Double(s), forKey: Self.key)
        print("zoom \(Int((s * 100).rounded()))%")
        NotificationCenter.default.post(name: Self.changed, object: nil, userInfo: ["was": was])
    }

    /// Handles ⌘ or ⌃ with +, = , − or 0, wherever in Pong it is pressed.
    func handle(_ e: NSEvent) -> Bool {
        let keys = e.modifierFlags.intersection([.command, .control, .option])
        guard keys == .command || keys == .control else { return false }
        switch e.charactersIgnoringModifiers {
        case "+", "=": zoomIn()
        case "-", "_": zoomOut()
        case "0": reset()
        default: return false
        }
        return true
    }
}

/// Clicks on the card act at once, even though the card is never the key window. It draws its
/// content at Pong's zoom: its bounds are its frame divided by the zoom, so everything inside,
/// AppKit views too, is drawn that much larger.
final class FirstMouseHostingView<Content: View>: NSHostingView<Content> {
    private var watching: NSObjectProtocol?

    required init(rootView: Content) {
        super.init(rootView: rootView)
        watching = NotificationCenter.default.addObserver(forName: Zoom.changed, object: nil, queue: .main) { [weak self] _ in
            MainActor.assumeIsolated { self?.zoom() }
        }
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) { fatalError("not used") }

    override func acceptsFirstMouse(for event: NSEvent?) -> Bool { true }

    override func setFrameSize(_ s: NSSize) {
        super.setFrameSize(s)
        zoom()
    }

    private func zoom() {
        let z = Zoom.shared.scale
        let b = NSSize(width: frame.width / z, height: frame.height / z)
        guard bounds.size != b else { return }
        setBoundsSize(b)
        needsLayout = true
    }
}
