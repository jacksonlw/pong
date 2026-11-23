import AppKit
import PongKit
import SwiftUI

/// Shows Pong's zoom above the Bar each time it changes, with controls to change it again. It
/// stays while the pointer is on it and goes a moment after the zoom stops changing.
@MainActor
final class ZoomControlWindow {
    private let panel: NSPanel
    private let host: ZoomedHostingView<ZoomControl>
    private var hide: Timer?
    private var pointerOn = false
    private var watching: [NSObjectProtocol] = []
    private static let gap: CGFloat = 8
    private static let shown: TimeInterval = 2.5

    init() {
        panel = NSPanel(contentRect: NSRect(x: 0, y: 0, width: 120, height: 30),
                        styleMask: CardPanelConfiguration.style, backing: .buffered, defer: false)
        CardPanelConfiguration.apply(to: panel)
        // Drawn at its own size, not Pong's zoom, so it stays the same as the zoom changes.
        host = ZoomedHostingView(ZoomControl(scale: 1, steps: Zoom.steps, choose: { _ in }), scale: 1)
        host.sizingOptions = []
        panel.contentView = host
        watching.append(NotificationCenter.default.addObserver(forName: Zoom.changed, object: nil, queue: .main) { [weak self] _ in
            // After the card has taken its new size.
            DispatchQueue.main.async { self?.show() }
        })
        for name in [NSWindow.didResizeNotification, NSWindow.didMoveNotification] {
            watching.append(NotificationCenter.default.addObserver(forName: name, object: nil, queue: .main) { [weak self] n in
                MainActor.assumeIsolated {
                    guard let self, self.panel.isVisible, n.object is CardPanel else { return }
                    self.place()
                }
            })
        }
    }

    private func control() -> ZoomControl {
        ZoomControl(scale: Zoom.shared.scale, steps: Zoom.steps, choose: { Zoom.shared.zoom(to: $0) },
                    hovering: { [weak self] in self?.pointer($0) })
    }

    private var card: NSWindow? { NSApp.windows.first { $0 is CardPanel && $0.isVisible } }

    private func show() {
        guard card != nil else { return }
        host.content = control()
        place()
        panel.orderFrontRegardless()
        print("zoom control shown at \(Int((Zoom.shared.scale * 100).rounded()))%")
        later()
    }

    /// Centred above the card, or below it when the card is at the top of the screen.
    private func place() {
        guard let card else { return hideNow() }
        let size = NSHostingView(rootView: control()).fittingSize
        let screen = card.screen?.visibleFrame ?? card.frame
        var y = card.frame.maxY + Self.gap
        if y + size.height > screen.maxY { y = card.frame.minY - Self.gap - size.height }
        let x = min(max(card.frame.midX - size.width / 2, screen.minX), screen.maxX - size.width)
        panel.setFrame(NSRect(x: x.rounded(), y: y.rounded(), width: size.width, height: size.height), display: true)
        if panel.parent !== card {
            panel.parent?.removeChildWindow(panel)
            card.addChildWindow(panel, ordered: .above)
        }
    }

    private func pointer(_ on: Bool) {
        pointerOn = on
        if on { hide?.invalidate() } else { later() }
    }

    /// Hides it soon. The timer runs only in the default mode, so it waits while the list of
    /// zooms is open.
    private func later() {
        hide?.invalidate()
        guard !pointerOn else { return }
        let t = Timer(timeInterval: Self.shown, repeats: false) { [weak self] _ in
            MainActor.assumeIsolated { self?.hideNow() }
        }
        RunLoop.main.add(t, forMode: .default)
        hide = t
    }

    private func hideNow() {
        hide?.invalidate()
        hide = nil
        pointerOn = false
        panel.parent?.removeChildWindow(panel)
        panel.orderOut(nil)
    }
}
