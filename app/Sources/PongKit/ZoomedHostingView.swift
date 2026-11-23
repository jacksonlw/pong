import AppKit
import SwiftUI

/// Lays `content` out at the size it is given divided by `scale`, then draws it `scale` times
/// larger from the top-left corner, so it fills that size, as a browser zooms a page.
public struct ZoomedRoot<Content: View>: View {
    var content: Content
    var scale: CGFloat

    public var body: some View {
        GeometryReader { g in
            content
                .frame(width: g.size.width / scale, height: g.size.height / scale)
                .scaleEffect(scale, anchor: .topLeading)
        }
    }
}

/// A window's SwiftUI content drawn at a zoom. The zoom is a SwiftUI scale, not a change of an
/// AppKit view's bounds: SwiftUI places clicks by its own geometry and ignores a scale between
/// its hosting view and the window, so clicks would land `scale` times too far from the corner.
/// AppKit views inside the content are scaled by SwiftUI too, so their clicks land as well.
/// Clicks act at once, even in a window that is not key.
public final class ZoomedHostingView<Content: View>: NSHostingView<ZoomedRoot<Content>> {
    public init(_ content: Content, scale: CGFloat) {
        super.init(rootView: ZoomedRoot(content: content, scale: scale))
    }

    @available(*, unavailable)
    public required init(rootView: ZoomedRoot<Content>) { super.init(rootView: rootView) }

    @available(*, unavailable)
    public required init?(coder: NSCoder) { fatalError("not used") }

    public var content: Content {
        get { rootView.content }
        set { rootView = ZoomedRoot(content: newValue, scale: scale) }
    }

    public var scale: CGFloat {
        get { rootView.scale }
        set { if newValue != rootView.scale { rootView = ZoomedRoot(content: content, scale: newValue) } }
    }

    public override func acceptsFirstMouse(for event: NSEvent?) -> Bool { true }
}
