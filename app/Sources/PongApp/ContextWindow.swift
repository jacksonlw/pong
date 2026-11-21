import AppKit
import PongKit
import SwiftUI

/// Shows the context beside the card while the pointer rests on the question, an answer or the
/// ring, starting level with it: a child window of the non-key card panel
///, never over the answers and never key.
@MainActor
final class ContextWindowController {
    private static var running: ContextWindowController?

    /// Starts following the Engine for Projects and Sessions and showing context beside the card.
    static func start(clientProvider: @escaping @Sendable () async -> EngineClient?) {
        let ctx = CardContext.live
        Task { await ctx.directory.follow(clientProvider: clientProvider) }
        running = ContextWindowController(ctx: ctx)
    }

    private let ctx: CardContext
    private let panel: NSPanel
    private let host: FirstMouseHostingView<AnyView?>
    private var shown: ContextHover.Target?
    private var pendingHide: DispatchWorkItem?
    /// How long the context stays while the pointer crosses from the card to it.
    private static let linger: TimeInterval = 0.25
    private static let gap: CGFloat = 8

    private init(ctx: CardContext) {
        self.ctx = ctx
        panel = NSPanel(contentRect: NSRect(x: 0, y: 0, width: ContextPanel.width, height: 100),
                        styleMask: CardPanelConfiguration.style, backing: .buffered, defer: false)
        CardPanelConfiguration.apply(to: panel)
        host = FirstMouseHostingView(rootView: nil)
        host.sizingOptions = [] // sized here, from the content's size at Pong's zoom
        panel.contentView = ZoomBox(host)
        track()
    }

    private func track() {
        withObservationTracking {
            _ = ctx.hover.target
            _ = ctx.hover.overPanel
            _ = ctx.directory.pings
        } onChange: { [weak self] in
            DispatchQueue.main.async {
                self?.update()
                self?.track()
            }
        }
    }

    private func update() {
        pendingHide?.cancel()
        guard let target = ctx.hover.target ?? (ctx.hover.overPanel ? shown : nil) else {
            let hide = DispatchWorkItem { [weak self] in self?.hide() }
            pendingHide = hide
            DispatchQueue.main.asyncAfter(deadline: .now() + Self.linger, execute: hide)
            return
        }
        guard let card = NSApp.windows.first(where: { $0 is CardPanel && $0.isVisible }),
              let content = ContextPanel(topic: target.topic, card: ctx.directory.card(for: target.ping)) else {
            hide()
            return
        }
        let hover = ctx.hover
        host.rootView = AnyView(content
            .background(HoverTracker(onChange: { hover.overPanel = $0 }))
            .environment(\.cardContext, ctx))
        // The shown host takes the size it is given, so it reports none (0 by 0, an invisible
        // window): the content is measured apart, by a host that sizes itself.
        let fit = NSHostingView(rootView: content.environment(\.cardContext, ctx)).fittingSize
        let z = Zoom.shared.scale
        let size = CGSize(width: (fit.width * z).rounded(), height: (fit.height * z).rounded())
        let screen = card.screen?.visibleFrame ?? card.frame
        var x = card.frame.minX - size.width - Self.gap
        if x < screen.minX { x = card.frame.maxX + Self.gap } // card at the left edge: show it on the right
        // Starts level with what the pointer rests on, so the eye moves straight across.
        let top = ctx.hover.top ?? card.frame.maxY
        let y = max(screen.minY, top - size.height) // near the screen's bottom: keep it on screen
        panel.setFrame(NSRect(x: x, y: y, width: size.width, height: size.height), display: true)
        if panel.parent !== card {
            panel.parent?.removeChildWindow(panel)
            card.addChildWindow(panel, ordered: .above)
        }
        panel.orderFrontRegardless()
        if shown != target { print("context shows \(target.ping.id) \(target.topic)") }
        shown = target
    }

    private func hide() {
        guard shown != nil else { return }
        shown = nil
        ctx.hover.overPanel = false
        panel.parent?.removeChildWindow(panel)
        panel.orderOut(nil)
        host.rootView = nil
    }
}
