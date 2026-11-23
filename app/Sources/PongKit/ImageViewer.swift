import AppKit
import SwiftUI

/// An image shown alone over the whole screen, from a click on it in a preview: the panel stays
/// as it is. Escape, a click anywhere or the close control puts it away. Its ink covers the whole
/// screen, but the image and its controls stay clear of the menu bar and the Dock.
@MainActor
public enum ImageViewer {
    private static var shown: Viewer?

    public static func show(_ image: NSImage, title: String) {
        shown?.dismiss()
        let mouse = NSEvent.mouseLocation
        guard let screen = NSScreen.screens.first(where: { NSMouseInRect(mouse, $0.frame, false) }) ?? NSScreen.main else { return }
        let v = Viewer(frame: screen.frame)
        v.contentView = NSHostingView(rootView: ImageViewerView(image: image, title: title, clear: FullScreen.clear(of: screen)) { v.dismiss() })
        v.onDismiss = { if shown === v { shown = nil } }
        shown = v
        v.present()
        print("image shown full screen")
    }

    final class Viewer: NSPanel {
        var onDismiss: () -> Void = {}

        init(frame: NSRect) {
            super.init(contentRect: frame, styleMask: [.borderless, .nonactivatingPanel], backing: .buffered, defer: false)
            isFloatingPanel = true
            level = .modalPanel
            collectionBehavior = [.canJoinAllSpaces, .fullScreenAuxiliary, .ignoresCycle]
            isOpaque = false
            backgroundColor = .clear
            hasShadow = false
            isReleasedWhenClosed = false
            hidesOnDeactivate = false
        }

        override var canBecomeKey: Bool { true }
        override func cancelOperation(_ sender: Any?) { dismiss() }

        private var reduceMotion: Bool { NSWorkspace.shared.accessibilityDisplayShouldReduceMotion }

        func present() {
            alphaValue = reduceMotion ? 1 : 0
            makeKeyAndOrderFront(nil)
            NSAnimationContext.runAnimationGroup { c in
                c.duration = reduceMotion ? 0 : 0.18
                animator().alphaValue = 1
            }
        }

        func dismiss() {
            onDismiss()
            NSAnimationContext.runAnimationGroup({ c in
                c.duration = reduceMotion ? 0 : 0.15
                animator().alphaValue = 0
            }, completionHandler: { [weak self] in
                MainActor.assumeIsolated { self?.orderOut(nil) }
            })
        }
    }
}

/// The image fitted to the screen on the kit's ink, never larger than it is until zoomed, with
/// the zoom controls and the close control. A click around the image puts it away; a click on it
/// does not, so a zoomed image can be looked over.
struct ImageViewerView: View {
    var image: NSImage
    var title: String
    /// How far in from each edge of the screen the menu bar and the Dock reach.
    var clear = EdgeInsets()
    var close: () -> Void

    var body: some View {
        ZStack(alignment: .topTrailing) {
            ZoomableImage(image: image, title: title, onInk: true, click: {}, fullScreen: true)
                .padding(48)
            IconButton(symbol: "xmark", label: "Close image", help: "Close (Esc)", colour: Brand.fixedWhite, action: close)
            .padding(16)
        }
        .padding(clear)
        .background {
            Brand.fixedInk
                .contentShape(Rectangle())
                .onTapGesture(perform: close)
        }
        .ignoresSafeArea()
    }
}

/// What a view over the whole screen keeps its content and controls clear of.
public enum FullScreen {
    /// How far in from each edge of `screen` the menu bar and the Dock reach: the whole screen less
    /// the part windows may use.
    public static func clear(of screen: NSScreen) -> EdgeInsets {
        clear(frame: screen.frame, visible: screen.visibleFrame)
    }

    static func clear(frame f: NSRect, visible v: NSRect) -> EdgeInsets {
        EdgeInsets(top: max(0, f.maxY - v.maxY), leading: max(0, v.minX - f.minX),
                   bottom: max(0, v.minY - f.minY), trailing: max(0, f.maxX - v.maxX))
    }
}
