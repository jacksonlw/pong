import AppKit
import SwiftUI

/// An image shown alone over the whole screen, from a click on it in a preview: the panel stays
/// as it is. Escape, a click anywhere or the close control puts it away.
@MainActor
public enum ImageViewer {
    private static var shown: Viewer?

    public static func show(_ image: NSImage, title: String) {
        shown?.dismiss()
        let mouse = NSEvent.mouseLocation
        guard let screen = NSScreen.screens.first(where: { NSMouseInRect(mouse, $0.frame, false) }) ?? NSScreen.main else { return }
        let v = Viewer(frame: screen.frame)
        v.contentView = NSHostingView(rootView: ImageViewerView(image: image, title: title) { v.dismiss() })
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

/// The image fitted to the screen on the kit's ink, with its title and the close control.
struct ImageViewerView: View {
    var image: NSImage
    var title: String
    var close: () -> Void

    var body: some View {
        ZStack(alignment: .topTrailing) {
            Brand.fixedInk
            Image(nsImage: image)
                .resizable()
                .aspectRatio(contentMode: .fit)
                .frame(maxWidth: image.size.width, maxHeight: image.size.height) // never larger than it is
                .padding(48)
                .frame(maxWidth: .infinity, maxHeight: .infinity)
                .accessibilityLabel(title)
            HoverButton(action: close) { hover in
                Image(systemName: "xmark").fontWeight(Brand.iconWeight).imageScale(.large).font(Brand.Style.label.font)
                    .foregroundStyle(Brand.fixedWhite)
                    .opacity(hover ? 1 : 0.7)
                    .frame(width: 32, height: 32)
                    .contentShape(Rectangle())
            }
            .help("Close (Esc)")
            .accessibilityLabel("Close image")
            .padding(16)
        }
        .contentShape(Rectangle())
        .onTapGesture(perform: close)
        .ignoresSafeArea()
    }
}
