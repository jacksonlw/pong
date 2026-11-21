import AppKit
import SwiftUI

/// The Threads, newest first, each opening its Thread: the list the Bar opens into.
/// A row shows its status as an icon at the left, and its Project's tile and name at the right.
/// A Thread open in a panel beside the card is drawn selected. Show more lists older Threads.
struct ThreadRows: View {
    var threads: [ThreadRow]
    var open: Set<SessionThread.SessionRef> = []
    /// Lists more Threads; nil when every one is listed.
    var more: (() -> Void)? = nil
    var openThread: (SessionThread.SessionRef) -> Void

    var body: some View {
        if threads.isEmpty {
            Text("No Threads yet.")
                .brandStyle(.secondary)
                .foregroundStyle(Brand.secondary)
                .fixedSize(horizontal: false, vertical: true)
        } else {
            VStack(spacing: 0) {
                ForEach(threads) { t in ThreadRowView(thread: t, isOpen: open.contains(t.ref), openThread: openThread) }
                if let more {
                    HoverButton(action: more) { hover in
                        Text("Show more").brandStyle(.label).foregroundStyle(hover ? Brand.text : Brand.secondary)
                            .padding(.horizontal, 10)
                            .frame(maxWidth: .infinity, minHeight: 32, alignment: .leading)
                            .background(hover ? Brand.row : .clear, in: Rectangle())
                            .contentShape(Rectangle())
                    }
                    .accessibilityHint("Lists older Threads")
                }
            }
        }
    }
}

/// One Thread row. Hovering it only highlights it: nothing opens beside it.
private struct ThreadRowView: View {
    var thread: ThreadRow
    /// Its Thread is open in a panel beside the card.
    var isOpen = false
    var openThread: (SessionThread.SessionRef) -> Void

    var body: some View {
        let t = thread
        HoverButton { openThread(t.ref) } label: { hover in
            HStack(spacing: 8) {
                ThreadStatusIcon(status: t.status)
                Text(t.title).brandStyle(.label).foregroundStyle(isOpen ? Brand.onSelected : Brand.text)
                    .lineLimit(1).truncationMode(.tail)
                Spacer(minLength: 8)
                if let p = t.project {
                    ProjectTile(project: p, logo: t.logo, side: 18)
                    Text(p.name).brandStyle(.secondary).foregroundStyle(isOpen ? Brand.onSelected : Brand.secondary)
                        .lineLimit(1).fixedSize()
                }
            }
            .padding(.horizontal, 10)
            .frame(minHeight: 32)
            .background(isOpen ? (hover ? Brand.selectedHover : Brand.selected) : hover ? Brand.row : .clear, in: Rectangle())
            .contentShape(Rectangle())
        }
        .accessibilityLabel([t.title, t.project?.name, t.status.label].compactMap { $0 }.joined(separator: ", "))
        .accessibilityValue(t.details?.summary ?? "")
        .accessibilityValue(isOpen ? "Open" : "")
        .accessibilityHint("Opens the Thread")
    }
}

extension ThreadStatus {
    /// The colour of the status's name and icon: blue, orange, green and red, deeper in light.
    var colour: Color {
        switch self {
        case .running: Brand.statusRunning
        case .waiting: Brand.statusWaiting
        case .done: Brand.statusDone
        case .error: Brand.statusError
        }
    }

    var symbol: String {
        switch self {
        case .running: "circle.dotted" // drawn as the spinner instead
        case .waiting: "exclamationmark.bubble"
        case .done: "checkmark.circle"
        case .error: "exclamationmark.triangle"
        }
    }
}

/// A Thread's status as a symbol in the status's colour; a spinner while the Session runs.
struct ThreadStatusIcon: View {
    var status: ThreadStatus

    var body: some View {
        Group {
            if status == .running {
                Spinner()
            } else {
                Image(systemName: status.symbol).imageScale(.large).font(Brand.Style.label.font).foregroundStyle(status.colour)
            }
        }
        .frame(width: 18, height: 18)
        .accessibilityHidden(true)
    }
}

/// A ring with an arc that turns once a second. It moves only while shown, and stays still
/// with Reduce Motion and in snapshots, as the typing dots do.
struct Spinner: View {
    var size: CGFloat = 15
    @Environment(\.typingAnimates) private var animates
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    var body: some View {
        Group {
            if animates && !reduceMotion {
                TurningRing()
            } else {
                ZStack {
                    Circle().stroke(Brand.track, lineWidth: 2)
                    Circle().trim(from: 0, to: 0.3)
                        .stroke(Brand.statusRunning, style: StrokeStyle(lineWidth: 2, lineCap: .round))
                        .rotationEffect(.degrees(-90))
                }
            }
        }
        .frame(width: size, height: size)
    }
}

/// The turning ring, turned by Core Animation rather than by SwiftUI drawing it again on every
/// frame: each SwiftUI redraw under the pointer puts the arrow back, which made the open hand
/// over the Bar flicker while a Session worked (as the card's edge line, `EdgeArea`, found).
private struct TurningRing: NSViewRepresentable {
    func makeNSView(context: Context) -> RingView { RingView() }
    func updateNSView(_ v: RingView, context: Context) {}

    final class RingView: NSView {
        private let track = CAShapeLayer()
        private let arc = CAShapeLayer()

        override init(frame: NSRect) {
            super.init(frame: frame)
            wantsLayer = true
            for l in [track, arc] {
                l.fillColor = nil
                l.lineWidth = 2
                layer?.addSublayer(l)
            }
            arc.lineCap = .round
            arc.strokeEnd = 0.3
        }

        required init?(coder: NSCoder) { fatalError("not used") }

        /// Clicks go through to whatever is behind, such as the Bar's drag area.
        override func hitTest(_ point: NSPoint) -> NSView? { nil }

        override func layout() {
            super.layout()
            let path = CGPath(ellipseIn: bounds.insetBy(dx: 1, dy: 1), transform: nil)
            for l in [track, arc] {
                l.frame = bounds
                l.path = path
            }
            colour()
        }

        override func viewDidChangeEffectiveAppearance() {
            super.viewDidChangeEffectiveAppearance()
            colour()
        }

        override func viewDidMoveToWindow() {
            super.viewDidMoveToWindow()
            guard window != nil, arc.animation(forKey: "turn") == nil else { return }
            let turn = CABasicAnimation(keyPath: "transform.rotation.z")
            turn.fromValue = 0
            turn.toValue = -2 * Double.pi // clockwise, as the SwiftUI ring turned
            turn.duration = 1
            turn.repeatCount = .infinity
            turn.isRemovedOnCompletion = false
            arc.add(turn, forKey: "turn")
        }

        private func colour() {
            effectiveAppearance.performAsCurrentDrawingAppearance {
                track.strokeColor = NSColor(Brand.track).cgColor
                arc.strokeColor = NSColor(Brand.statusRunning).cgColor
            }
        }
    }
}
