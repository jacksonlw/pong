import AppKit
import SwiftUI
import PongKit

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
    func zoom(to s: CGFloat) { set(s) }

    /// Every window's content, drawn at Pong's zoom.
    private var hosts: [() -> (any ZoomedHost)?] = []

    /// Hosts a window's content at Pong's zoom, which it follows as the zoom changes.
    func host<Content: View>(_ content: Content) -> ZoomedHostingView<Content> {
        let h = ZoomedHostingView(content, scale: scale)
        hosts.removeAll { $0() == nil }
        hosts.append { [weak h] in h }
        return h
    }

    private func set(_ s: CGFloat) {
        guard s != scale else { return }
        let was = scale
        scale = s
        for h in hosts { h()?.scale = s }
        UserDefaults.standard.set(Double(s), forKey: Self.key)
        print("zoom \(Int((s * 100).rounded()))%")
        NotificationCenter.default.post(name: Self.changed, object: nil, userInfo: ["was": was])
    }

    /// Handles ⌘ or ⌃ with +, = , − or 0, wherever in Pong it is pressed; over an image, or with
    /// one full screen, they zoom only that image.
    func handle(_ e: NSEvent) -> Bool {
        guard let key = ZoomKey(e) else { return false }
        if ImageZoomKeys.handle(e) { return true }
        switch key {
        case .in: zoomIn()
        case .out: zoomOut()
        case .reset: reset()
        }
        return true
    }
}

/// A hosting view that follows Pong's zoom.
@MainActor
private protocol ZoomedHost: AnyObject {
    var scale: CGFloat { get set }
}

extension ZoomedHostingView: ZoomedHost {}
