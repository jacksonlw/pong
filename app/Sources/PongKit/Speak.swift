import Foundation
import Observation
import SwiftUI

// Speak: what the human says becomes text in the box they are typing in. Pong reads nothing
// aloud and sends nothing; the human sends what was written as if typed. Hearing is the Mac's,
// through the same VoiceIO as Voice mode, which stops listening while Speak listens.

/// A box Speak can write into.
public enum SpeakBox: Hashable, Sendable {
    /// The start panel.
    case start
    /// The card's reply box, by Ping.
    case ping(String)
    /// A Thread's reply box, by Session ("harness/id").
    case session(String)
}

/// Where Speak's words go in a box's text: in place of nothing at the caret, the finished
/// utterances followed by the one still being heard, each apart from the text around them.
struct SpeakWriting: Equatable {
    var before: String
    var after: String
    var fixed = ""
    var live = ""

    /// Writing starts at the caret, an offset in UTF-16 units as the text view counts; nil or
    /// past the end is the end.
    init(text: String, caret: Int?) {
        let ns = text as NSString
        let at = min(max(caret ?? ns.length, 0), ns.length)
        before = ns.substring(to: at)
        after = ns.substring(from: at)
    }

    var spoken: String { [fixed, live].filter { !$0.isEmpty }.joined(separator: " ") }

    var text: String {
        let words = spoken
        guard !words.isEmpty else { return before + after }
        let lead = before.isEmpty || before.last!.isWhitespace ? "" : " "
        let trail = after.isEmpty || after.first!.isWhitespace ? "" : " "
        return before + lead + words + trail + after
    }

    mutating func heard(partial: String) { live = partial }

    mutating func heard(words: String) {
        fixed = [fixed, words].filter { !$0.isEmpty }.joined(separator: " ")
        live = ""
    }
}

/// The microphone beside Start Session and in each reply box on the Mac. One box at a time.
@MainActor @Observable
public final class Speak {
    /// The box Speak writes into while it listens; nil while it does not.
    public private(set) var box: SpeakBox?
    public private(set) var level: Float = 0
    /// Why Speak could not listen, and in which box, until it starts again.
    public private(set) var problem: (box: SpeakBox, text: String)?
    public var listening: Bool { box != nil }

    /// Speak stops by itself after this, so a microphone left on is not left on.
    public static let limit: Duration = .seconds(5 * 60)

    @ObservationIgnored let io: VoiceIO
    @ObservationIgnored let permissions: VoicePermissions?
    @ObservationIgnored let sleep: @MainActor (Duration) async -> Void
    /// Voice mode, which stops listening when Speak starts.
    @ObservationIgnored public weak var voice: VoiceSession?
    @ObservationIgnored private var writing: SpeakWriting?
    @ObservationIgnored private var written = ""
    @ObservationIgnored private var read: () -> String = { "" }
    @ObservationIgnored private var write: (String) -> Void = { _ in }
    @ObservationIgnored private var cap: Task<Void, Never>?
    /// The boxes on screen that Speak can write into, for the shortcut.
    @ObservationIgnored private var boxes: [SpeakBox: Attached] = [:]
    @ObservationIgnored private var focused: SpeakBox?
    /// The box the shortcut opened and starts in once it is on screen.
    @ObservationIgnored private var wanted: SpeakBox?

    struct Attached {
        var read: () -> String
        var write: (String) -> Void
        var caret: () -> Int?
    }
    @ObservationIgnored private var asking: Task<Void, Never>?

    public init(io: VoiceIO, permissions: VoicePermissions? = nil,
                sleep: @escaping @MainActor (Duration) async -> Void = { try? await Task.sleep(for: $0) }) {
        self.io = io
        self.permissions = permissions
        self.sleep = sleep
    }

    /// The microphone button and the shortcut: starts Speak in the box, or stops it there.
    public func toggle(_ b: SpeakBox, text: Binding<String>, caret: Int?) {
        if box == b { stop() } else { start(b, read: { text.wrappedValue }, write: { text.wrappedValue = $0 }, caret: caret) }
    }

    /// Starts writing what is heard into a box at the caret; another box's Speak stops.
    public func start(_ b: SpeakBox, read: @escaping () -> String, write: @escaping (String) -> Void, caret: Int?) {
        stop()
        problem = nil
        asking = Task {
            if let permissions, let refused = await permissions.request() {
                problem = (b, refused)
                return
            }
            guard !Task.isCancelled else { return }
            voice?.stop()
            self.read = read
            self.write = write
            let now = read()
            writing = SpeakWriting(text: now, caret: caret)
            written = now
            box = b
            io.listen { [weak self] h in self?.heard(h) }
            cap = Task {
                await sleep(Self.limit)
                if !Task.isCancelled { stop() }
            }
        }
    }

    // MARK: The shortcut

    /// A box on screen that can be written into now; `caret` says where its caret is.
    public func attach(_ b: SpeakBox, read: @escaping () -> String, write: @escaping (String) -> Void, caret: @escaping () -> Int?) {
        boxes[b] = Attached(read: read, write: write, caret: caret)
        if wanted == b {
            wanted = nil
            start(b, read: read, write: write, caret: caret())
        }
    }

    /// The box is gone or cannot be written into; Speak stops in it.
    public func detach(_ b: SpeakBox) {
        boxes[b] = nil
        if focused == b { focused = nil }
        stop(b)
    }

    /// The box took or lost typing focus.
    public func focus(_ b: SpeakBox, _ on: Bool) {
        if on { focused = b } else if focused == b { focused = nil }
    }

    /// Which box the shortcut starts in: the one with focus, else a waiting card's reply box;
    /// nil when neither is on screen, and the start panel is opened.
    static func target(focused: SpeakBox?, on screen: Set<SpeakBox>) -> SpeakBox? {
        if let focused, screen.contains(focused) { return focused }
        return screen.filter { if case .ping = $0 { true } else { false } }.sorted { "\($0)" < "\($1)" }.first
    }

    /// The shortcut: stops Speak while it listens; otherwise starts it in the box `target`
    /// chooses, or opens the start panel and starts there once it shows.
    public func shortcut(openStart: () -> Void) {
        if listening {
            stop()
            return
        }
        guard let b = Self.target(focused: focused, on: Set(boxes.keys)), let a = boxes[b] else {
            wanted = .start
            openStart()
            if let a = boxes[.start] {
                wanted = nil
                start(.start, read: a.read, write: a.write, caret: a.caret())
            }
            return
        }
        start(b, read: a.read, write: a.write, caret: a.caret())
    }

    /// Stops listening, in any box or only in `b`. What was written stays; nothing is sent.
    public func stop(_ b: SpeakBox? = nil) {
        if let b, b != box { return }
        asking?.cancel()
        asking = nil
        cap?.cancel()
        cap = nil
        if box != nil { io.stopListening() }
        box = nil
        level = 0
        writing = nil
        read = { "" }
        write = { _ in }
    }

    private func heard(_ h: Heard) {
        guard box != nil, var w = writing else { return }
        switch h {
        case .level(let l):
            level = l
            return
        case .failed(let why):
            let b = box!
            stop()
            problem = (b, "Speech recognition stopped: \(why)")
            return
        default: break
        }
        // Typed into while listening: what is heard next goes after what is there now.
        let now = read()
        if now != written { w = SpeakWriting(text: now, caret: nil) }
        switch h {
        case .partial(let p): w.heard(partial: p)
        case .words(let words): w.heard(words: words)
        default: break
        }
        writing = w
        written = w.text
        write(written)
    }
}

/// The microphone: a 40-point tile, the same beside Start Session and in each reply row. It
/// shows `mic` while idle and the level meter while Speak listens in its box.
struct SpeakButton: View {
    var speak: Speak
    var box: SpeakBox
    var enabled: Bool = true
    var toggle: () -> Void

    var body: some View {
        let on = speak.box == box
        HoverButton(action: toggle) { hover in
            ZStack {
                if on {
                    LevelMeter(level: speak.level)
                } else {
                    Image(systemName: "mic").imageScale(.large)
                        .font(Brand.Style.label.font)
                        .brandIcon()
                }
            }
            .frame(width: 40, height: 40)
            .foregroundStyle(enabled || on ? Brand.text : Brand.secondary)
            .background(hover && (enabled || on) ? Brand.rowHover : Brand.row, in: Rectangle())
            .contentShape(Rectangle())
        }
        .disabled(!enabled && !on)
        .help(on ? "Stop. What you said stays in the box." : "Speak into the box. Nothing is sent until you send it.")
        .accessibilityLabel(on ? "Stop speaking" : "Speak")
    }
}
