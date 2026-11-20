import Foundation
import Observation

/// What the card's middle shows: a Ping, or, after answering one that said more are coming,
/// that the next question is being written.
public struct CardScene: Equatable, Sendable {
    public var ping: Ping
    public var writing: Bool
    /// Where the Ping is in the deck; moving back or forth slides the content that way.
    public var position: Int

    public init(ping: Ping, writing: Bool = false, position: Int = 0) {
        self.ping = ping
        self.writing = writing
        self.position = position
    }

    /// Scenes with the same key are the same content, updated in place without a fade.
    public var key: String { writing ? "\(ping.id)+writing" : ping.id }
}

/// Sequences the card's changes so old and new content are never on screen together:
/// a chosen answer is held as chosen, the old content fades out fully, the new content
/// replaces it, the card's box starts easing to the new content's height, and once the box
/// is most of the way there the new content fades in. The content itself never changes size.
/// It always ends on the latest scene it was given, however quickly scenes arrive.
@MainActor @Observable
public final class CardStage {
    public struct Timing: Sendable {
        /// `settle` is how long the box eases before the new content starts to fade in;
        /// `box` is the whole of the box's ease.
        public var hold, fadeOut, settle, fadeIn, box: Duration

        public init(hold: Duration, fadeOut: Duration, settle: Duration, fadeIn: Duration, box: Duration) {
            self.hold = hold
            self.fadeOut = fadeOut
            self.settle = settle
            self.fadeIn = fadeIn
            self.box = box
        }

        /// Polaris's short durations: out in 100 ms, the box eased over 220 ms, the new
        /// content in over 200 ms from 50 ms into the box's ease; a chosen answer held 0.3 s.
        public static let standard = Timing(hold: .milliseconds(300), fadeOut: .milliseconds(100), settle: .milliseconds(50),
                                            fadeIn: .milliseconds(200), box: .milliseconds(220))
        /// Reduce Motion: no fades or easing; a chosen answer is still shown before the card moves on.
        public static let reduced = Timing(hold: .milliseconds(300), fadeOut: .zero, settle: .zero, fadeIn: .zero, box: .zero)

        /// A duration in seconds, for SwiftUI's animations.
        public static func seconds(_ d: Duration) -> Double {
            Double(d.components.seconds) + Double(d.components.attoseconds) / 1e18
        }
    }

    /// The content on screen.
    public private(set) var shown: CardScene?
    /// Whether that content is visible (false while it fades out and before it fades in).
    public private(set) var visible = true
    /// Which side the content sits off to while hidden, in steps: -1 left, 1 right, 0 none.
    /// Going forward the old content leaves to the left and the new enters from the right;
    /// going back the other way. It is 0 whenever the content is visible.
    public private(set) var slide = 0
    /// The height of the shown content as laid out; the card eases to it.
    public private(set) var height: CGFloat?

    public var timing: Timing
    @ObservationIgnored private var target: CardScene?
    /// A chosen answer is being held on screen.
    @ObservationIgnored private var holding = false
    @ObservationIgnored private var running: Task<Void, Never>?
    @ObservationIgnored private let sleep: @MainActor (Duration) async -> Void
    /// Called after each change of `shown` or `visible`, for tests.
    @ObservationIgnored package var onStep: ((CardScene?, Bool) -> Void)?

    public init(timing: Timing = .standard, sleep: @escaping @MainActor (Duration) async -> Void = { try? await Task.sleep(for: $0) }) {
        self.timing = timing
        self.sleep = sleep
    }

    /// The scene the card should end on; nil hides the card.
    public func set(_ scene: CardScene?) {
        target = scene
        if running != nil { return } // the running sequence ends on the latest target
        if shown == nil, let scene { // a hidden card opens at once
            show(scene, visible: true)
        } else if let scene, scene.key == shown?.key {
            shown = scene // same content, e.g. new Resources: no fade
        } else if shown != nil {
            running = Task { await self.sequence() }
        }
    }

    /// The human chose an answer: it shows as chosen before the card moves on. A reply sent
    /// from the reply row has nothing to show as chosen, so the card moves on as it does
    /// for the arrows; text typed alongside a chosen answer is not shown either.
    public func choose(_ pong: Ping.Pong) {
        guard let answer = pong.answer, var s = shown, running == nil else { return }
        s.ping.pong = Ping.Pong(answer: answer)
        shown = s
        holding = true
        running = Task { await self.sequence() } // after the hold, whatever the Engine reports wins
    }

    /// The laid-out height of a scene's content; only the shown scene's counts.
    public func measured(_ h: CGFloat, for key: String) {
        if key == shown?.key, h > 0 { height = h }
    }

    private func sequence() async {
        if holding {
            await sleep(timing.hold)
            holding = false
        }
        while target?.key != shown?.key {
            if visible {
                slide = -step(to: target)
                visible = false
                onStep?(shown, visible)
                await sleep(timing.fadeOut)
                continue // the target may have changed while fading out
            }
            guard let t = target else {
                show(nil, visible: false)
                break
            }
            slide = step(to: t)
            show(t, visible: false)
            await sleep(timing.settle) // the box is most of the way to the new height before anything shows
            if target?.key != t.key { continue } // a newer scene replaces it, still hidden
            slide = 0
            visible = true
            onStep?(shown, visible)
            await sleep(timing.fadeIn)
        }
        if let t = target, !visible || shown != t {
            slide = 0
            shown = t
            visible = true
            onStep?(shown, visible)
        }
        running = nil
    }

    /// The way from the shown content to a scene: 1 forward in the deck, -1 back, 0 neither.
    private func step(to scene: CardScene?) -> Int {
        guard let from = shown?.position, let to = scene?.position else { return 0 }
        return to > from ? 1 : to < from ? -1 : 0
    }

    private func show(_ scene: CardScene?, visible v: Bool) {
        if scene == nil { slide = 0 }
        if scene == nil { height = nil } // otherwise the old height stays until the new content is laid out
        shown = scene
        visible = v
        onStep?(shown, visible)
    }
}
