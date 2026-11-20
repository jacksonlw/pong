import AppKit
import SwiftUI

/// The Threads, newest first, each opening its Thread: the list the Bar opens into.
/// A row shows its status as an icon at the left, and its Project's tile and name at the right.
struct ThreadRows: View {
    var threads: [ThreadRow]
    var openThread: (SessionThread.SessionRef) -> Void

    var body: some View {
        if threads.isEmpty {
            Text("No Threads yet. A Thread appears once a Session starts or sends a Ping.")
                .brandStyle(.secondary)
                .foregroundStyle(Brand.secondary)
                .fixedSize(horizontal: false, vertical: true)
        } else {
            VStack(spacing: 0) {
                ForEach(threads) { t in ThreadRowView(thread: t, openThread: openThread) }
            }
        }
    }
}

/// One Thread row. Once the pointer has rested on it a moment, its details open beside it.
private struct ThreadRowView: View {
    var thread: ThreadRow
    var openThread: (SessionThread.SessionRef) -> Void
    @State private var resting = false
    @State private var showsDetails = false

    /// How long the pointer rests on a row before its details open, so passing over rows opens none.
    static let rest: Duration = .milliseconds(450)

    var body: some View {
        let t = thread
        HoverButton { openThread(t.ref) } label: { hover in
            HStack(spacing: 8) {
                ThreadStatusIcon(status: t.status)
                Text(t.title).brandStyle(.label).lineLimit(1).truncationMode(.tail)
                Spacer(minLength: 8)
                if let p = t.project {
                    ProjectTile(project: p, logo: t.logo, side: 18)
                    Text(p.name).brandStyle(.secondary).foregroundStyle(Brand.secondary)
                        .lineLimit(1).fixedSize()
                }
            }
            .padding(.horizontal, 10)
            .frame(minHeight: 32)
            .background(hover ? Brand.row : .clear, in: Rectangle())
            .contentShape(Rectangle())
        }
        .background(HoverTracker { resting = $0 })
        .task(id: resting) {
            guard resting else { showsDetails = false; return }
            try? await Task.sleep(for: Self.rest)
            if !Task.isCancelled, resting { showsDetails = true }
        }
        .popover(isPresented: Binding(get: { showsDetails && hasDetails }, set: { if !$0 { showsDetails = false } }),
                 arrowEdge: .trailing) {
            if let d = t.details { ThreadDetailsView(details: d).presentationBackground(Brand.surface) }
        }
        .accessibilityLabel([t.title, t.project?.name, t.status.label].compactMap { $0 }.joined(separator: ", "))
        .accessibilityValue(t.details?.summary ?? "")
        .accessibilityHint("Opens the Thread")
    }

    private var hasDetails: Bool { !(thread.details?.sections(now: Date()).isEmpty ?? true) }
}

/// A Thread's details: the agent's latest summary, when the Session started and how long it has
/// run, and how full its context is. The times count up each second.
struct ThreadDetailsView: View {
    var details: ThreadDetails
    /// Fixed in snapshots; otherwise the current time, every second while it counts.
    var now: Date? = nil
    @Environment(\.calendar) private var calendar
    @Environment(\.locale) private var locale

    static let width: CGFloat = 300

    var body: some View {
        Group {
            if let now { list(now) } else if details.counts {
                TimelineView(.periodic(from: .now, by: 1)) { list($0.date) }
            } else { list(Date()) }
        }
        .monospacedDigit() // the counting times keep their width as they tick
        .padding(14)
        .frame(width: Self.width, alignment: .leading)
        .fixedSize(horizontal: false, vertical: true)
        .background(Brand.surface)
    }

    private func list(_ at: Date) -> some View { ContextSectionList(sections: details.sections(now: at, calendar: calendar, locale: locale)) }
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
