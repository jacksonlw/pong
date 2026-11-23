import Foundation
import Observation

// Voice mode: Pong reads the card's Ping aloud and listens for the human's answer.
// Hearing and speaking happen on the Mac (VoiceIO); only understanding may use a model.

/// What the microphone reports while listening.
public enum Heard: Equatable, Sendable {
    /// The input level, 0 (silence) to 1.
    case level(Float)
    /// One finished utterance, as text.
    case words(String)
    /// The utterance still being heard, as far as it goes; it ends as `words`.
    case partial(String)
    /// Recognition stopped working, in macOS's words.
    case failed(String)
}

/// Hearing and speaking on the Mac. The app uses `SystemVoiceIO`; tests use a fake.
@MainActor
public protocol VoiceIO: AnyObject {
    /// Speaks through the system voice and returns when done or stopped.
    func speak(_ text: String) async
    func stopSpeaking()
    /// Starts the microphone; `onHeard` gets levels and utterances until `stopListening`.
    func listen(_ onHeard: @escaping @MainActor (Heard) -> Void)
    func stopListening()
}

/// Asks macOS for the microphone and speech recognition. Nil when both are granted, else what
/// the human should do to grant them.
public protocol VoicePermissions: Sendable {
    func request() async -> String?
}

/// What voice can do to the card's Ping; the card wires these to the Inbox.
public struct VoiceActions {
    public var send: (Ping.Pong, Ping) -> Void
    public var dismiss: (Ping) -> Void
    /// Opens (true) or closes the context panel beside the card.
    public var showContext: (Ping, Bool) -> Void
    public var openResource: (Resource, Ping) -> Void

    public init(send: @escaping (Ping.Pong, Ping) -> Void = { _, _ in }, dismiss: @escaping (Ping) -> Void = { _ in },
                showContext: @escaping (Ping, Bool) -> Void = { _, _ in }, openResource: @escaping (Resource, Ping) -> Void = { _, _ in }) {
        self.send = send
        self.dismiss = dismiss
        self.showContext = showContext
        self.openResource = openResource
    }
}

/// The words Pong speaks.
public enum VoiceScript {
    /// The question, then the answers numbered, saying which one is recommended (the first,
    /// when there is a choice: the Engine puts the recommended answer first).
    public static func reading(_ ping: Ping) -> String {
        var parts = [ping.question]
        let answers = (ping.answers ?? []).map(\.label)
        if ping.isNote == true || answers.isEmpty { return ping.question }
        if ping.allowSeveral == true { parts.append("Choose any that apply.") }
        for (i, a) in answers.enumerated() {
            let recommended = i == 0 && answers.count > 1 && a != "Nothing more"
            parts.append("\(i + 1): \(a)\(recommended ? ", recommended" : "").")
        }
        return parts.joined(separator: " ")
    }
}

/// Voice for the card's Ping: reads it, listens with the level shown on the card, and acts on
/// what it hears. Listening ends after a Pong is sent, after 30 seconds of silence, or on "stop".
@MainActor @Observable
public final class VoiceSession {
    /// Voice mode is on, and listening starts as a Ping appears (both from the Engine's settings).
    public var on = false {
        didSet { if !on { stop() } }
    }
    public var autoListen = false
    /// The microphone is on. The card shows the level meter and stop button while it is.
    public private(set) var listening = false
    public private(set) var level: Float = 0
    /// The card's Ping.
    public private(set) var ping: Ping?
    /// What failed, said once and shown on the card until the next Ping.
    public private(set) var problem: String?
    /// Hosted voice has said once that it needs Pong Cloud.
    @ObservationIgnored private var toldLapsed = false

    /// The answer being filled in before it is sent.
    public let fill: VoiceFill
    @ObservationIgnored public var actions = VoiceActions()
    /// The chosen model, or nil while none is set.
    @ObservationIgnored public var model: @MainActor () async -> VoiceModel? = { nil }
    /// A Ping's quick context, from the Directory.
    @ObservationIgnored public var quickContext: @MainActor (Ping) -> String? = { _ in nil }
    @ObservationIgnored let io: VoiceIO
    @ObservationIgnored let sleep: @MainActor (Duration) async -> Void
    @ObservationIgnored private var silence: Task<Void, Never>?
    @ObservationIgnored private var reading: Task<Void, Never>?
    /// Pong is speaking; what the microphone hears meanwhile is Pong itself and is ignored.
    @ObservationIgnored private var speaking = false

    public static let silenceLimit: Duration = .seconds(30)

    public init(io: VoiceIO, sleep: @escaping @MainActor (Duration) async -> Void = { try? await Task.sleep(for: $0) }) {
        self.io = io
        self.sleep = sleep
        fill = VoiceFill(sleep: sleep)
        fill.onSend = { [weak self] pong in
            guard let self, let p = self.ping else { return }
            self.stop() // listening ends once a Pong is sent
            self.actions.send(pong, p)
        }
    }

    /// The card now shows this Ping (nil when it is hidden). A new waiting Ping is read and,
    /// with auto-listen, listened for; a Ping that stops waiting ends listening.
    public func show(_ next: Ping?) {
        if let next, next.id == ping?.id {
            ping = next
            if !next.isWaiting { stop() }
            return
        }
        stop()
        fill.reset()
        ping = next
        problem = nil
        if on, autoListen, next?.isWaiting == true { start() }
    }

    /// The microphone button: reads the Ping, then listens.
    public func start() {
        guard on, let p = ping, p.isWaiting, !p.isThought, !listening, reading == nil else { return }
        reading = Task {
            await say(VoiceScript.reading(p))
            reading = nil
            guard !Task.isCancelled, ping?.id == p.id else { return }
            listen()
        }
    }

    /// The stop button, "stop", a sent Pong, silence or another Ping.
    public func stop() {
        fill.cancel() // nothing is sent once Pong stops listening
        reading?.cancel()
        reading = nil
        silence?.cancel()
        silence = nil
        if speaking { io.stopSpeaking() }
        if listening { io.stopListening() }
        listening = false
        level = 0
    }

    private func listen() {
        listening = true
        io.listen { [weak self] h in self?.heard(h) }
        restartSilence()
    }

    func restartSilence() {
        silence?.cancel()
        silence = Task {
            await sleep(Self.silenceLimit)
            if !Task.isCancelled { stop() }
        }
    }

    func say(_ text: String) async {
        speaking = true
        await io.speak(text)
        speaking = false
    }

    private func heard(_ h: Heard) {
        guard listening else { return }
        switch h {
        case .level(let l): level = l
        case .partial: break // Voice mode acts on finished utterances only
        case .failed(let why): fail("Speech recognition stopped: \(why)")
        case .words(let w):
            guard !speaking else { return }
            restartSilence()
            Task { await self.handle(w) }
        }
    }

    func handle(_ words: String) async {
        guard let p = ping else { return }
        let w = VoiceUnderstanding.normalized(words)
        if w == "stop" { stop(); return }
        if p.isNote == true {
            if w == "dismiss" || w == "dismiss it" {
                stop()
                actions.dismiss(p)
            }
            return // a Note is listened to for "dismiss" only
        }
        var model = await model()
        var intent: VoiceIntent
        do {
            intent = try await VoiceUnderstanding.understand(words, VoicePing(p, quickContext: quickContext(p)),
                                                             pending: fill.active, model: model)
        } catch is HostedVoiceLapsed {
            // Hosted voice lapsed: say so once and carry on with the phrases matched on the Mac.
            model = nil
            intent = VoiceUnderstanding.local(words, VoicePing(p, quickContext: quickContext(p)), pending: fill.active) ?? .notUnderstood
            if !toldLapsed {
                toldLapsed = true
                await say(HostedVoiceLapsed.said)
                if case .notUnderstood = intent { return }
            }
        } catch {
            fail("The voice model failed: \(error.localizedDescription)")
            return
        }
        guard listening, ping?.id == p.id else { return }
        switch intent {
        case .stop: stop()
        case .answer(let choices, let text):
            if choices.count > 1 && p.allowSeveral != true {
                fill.cancel()
                await say("Which one: \(choices.joined(separator: ", or "))?")
            } else if p.allowSeveral == true {
                fill.start(Ping.Pong(answers: choices.isEmpty ? nil : choices, text: text))
            } else {
                fill.start(Ping.Pong(answer: choices.first, text: text))
            }
        case .wait: fill.wait()
        case .go: fill.go()
        case .cancel: fill.cancel()
        case .notUnderstood:
            await say(model == nil ? "I didn't understand. Say a number, like \"the first one\"." : "I didn't understand.")
        case .repeatPing: await say(VoiceScript.reading(p))
        case .explain:
            let vp = VoicePing(p, quickContext: quickContext(p))
            do {
                await say(try await VoiceUnderstanding.explain(vp, model: model))
            } catch is HostedVoiceLapsed {
                await say((try? await VoiceUnderstanding.explain(vp, model: nil)) ?? "")
            } catch {
                fail("The voice model failed: \(error.localizedDescription)")
                return
            }
        case .showContext: actions.showContext(p, true)
        case .hideContext: actions.showContext(p, false)
        case .showResource(let name):
            let found = VoiceUnderstanding.resources(named: name, in: p.resources ?? [])
            switch found.count {
            case 0: await say("This Ping has no \(name).")
            case 1: actions.openResource(found[0], p)
            default: await say("Which one: \(found.map(\.title).joined(separator: ", or "))?")
            }
        case .dismiss: break
        }
        // Listening continues after each of these, with the silence timer started again.
        if listening { restartSilence() }
    }

    /// A click on the card: the bar stops and the human finishes the answer.
    public func takeOver() {
        guard fill.active else { return }
        fill.takeOver()
        stop()
    }

    /// Fails open: says what went wrong once, stops listening, leaves the card as it was.
    func fail(_ why: String) {
        stop()
        guard problem == nil else { return }
        problem = why
        Task { await say(why) }
    }
}

public enum VoiceUnderstanding {
    /// Lower case, without punctuation, single spaces.
    public static func normalized(_ s: String) -> String {
        let kept = s.lowercased().map { $0.isLetter || $0.isNumber || $0 == "'" ? $0 : " " }
        return String(kept).split(separator: " ").joined(separator: " ")
    }
}
