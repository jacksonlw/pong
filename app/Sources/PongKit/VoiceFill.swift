import Foundation
import Observation

/// The slow fill: an understood answer shows its choices at once and
/// writes its text into the draft at reading pace, then a three-second bar fills and the Pong
/// is sent. "Wait" pauses until "go"; "cancel" clears it; a new answer starts again; a click
/// on the card hands it to the human. Driven by an injected clock, as CardStage is.
@MainActor @Observable
public final class VoiceFill {
    public enum Phase: Equatable, Sendable {
        case idle, filling, countingDown, sent
        /// The human clicked the card: the choices and text stay for them to finish.
        case takenOver
    }

    public private(set) var phase: Phase = .idle
    /// Paused by "wait", while filling or counting down.
    public private(set) var paused = false
    /// The chosen answers, highlighted on the card.
    public private(set) var choices: [String] = []
    /// The text written into the draft so far.
    public private(set) var text = ""
    /// How full the countdown bar is, 0 to 1.
    public private(set) var progress: Double = 0

    public static let countdown: Duration = .seconds(3)
    public static let tick: Duration = .milliseconds(100)
    /// About 250 words a minute.
    public static let perCharacter: Duration = .milliseconds(48)

    /// Filling or counting down: an answer is on its way.
    public var active: Bool { phase == .filling || phase == .countingDown }

    @ObservationIgnored private let sleep: @MainActor (Duration) async -> Void
    @ObservationIgnored public var onSend: (Ping.Pong) -> Void = { _ in }
    @ObservationIgnored private var generation = 0
    @ObservationIgnored private var resume: CheckedContinuation<Void, Never>?

    public init(sleep: @escaping @MainActor (Duration) async -> Void = { try? await Task.sleep(for: $0) }) {
        self.sleep = sleep
    }

    /// Starts filling in `pong`, replacing any answer on its way.
    public func start(_ pong: Ping.Pong) {
        stopRunning()
        choices = pong.answer.map { [$0] } ?? pong.answers ?? []
        text = ""
        progress = 0
        paused = false
        phase = .filling
        let g = generation
        Task { await run(pong, g) }
    }

    public func wait() { if active { paused = true } }

    public func go() {
        guard paused else { return }
        paused = false
        wake()
    }

    /// Clears the answer.
    public func cancel() {
        guard active else { return }
        stopRunning()
        clear()
    }

    /// The human clicked the card: the bar stops and the answer is theirs to finish.
    public func takeOver() {
        guard active else { return }
        stopRunning()
        phase = .takenOver
    }

    /// Forgets a sent or taken-over answer, for the next Ping.
    public func reset() {
        stopRunning()
        clear()
    }

    private func clear() {
        phase = .idle
        choices = []
        text = ""
        progress = 0
    }

    private func stopRunning() {
        generation += 1
        paused = false
        wake()
    }

    private func wake() {
        resume?.resume()
        resume = nil
    }

    /// Waits one step, and while paused; false when the answer was replaced or stopped meanwhile.
    private func step(_ d: Duration, _ g: Int) async -> Bool {
        await sleep(d)
        while paused && g == generation {
            await withCheckedContinuation { resume = $0 }
        }
        return g == generation
    }

    private func run(_ pong: Ping.Pong, _ g: Int) async {
        for c in pong.text ?? "" {
            guard await step(Self.perCharacter, g) else { return }
            text.append(c)
        }
        phase = .countingDown
        let ticks = Int(Self.countdown / Self.tick)
        for i in 1...ticks {
            guard await step(Self.tick, g) else { return }
            progress = Double(i) / Double(ticks)
        }
        phase = .sent
        onSend(pong)
    }
}
