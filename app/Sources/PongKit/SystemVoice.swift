import AVFoundation
import Speech

/// The microphone and speech recognition permissions, as macOS grants them.
public struct SystemVoicePermissions: VoicePermissions {
    public init() {}

    public func request() async -> String? {
        let mic = await AVCaptureDevice.requestAccess(for: .audio)
        let speech = await withCheckedContinuation { c in
            SFSpeechRecognizer.requestAuthorization { c.resume(returning: $0 == .authorized) }
        }
        switch (mic, speech) {
        case (true, true): return nil
        case (false, true): return "Pong needs the microphone. Open System Settings > Privacy & Security > Microphone and switch Pong on."
        case (true, false): return "Pong needs speech recognition. Open System Settings > Privacy & Security > Speech Recognition and switch Pong on."
        case (false, false): return "Pong needs the microphone and speech recognition. Open System Settings > Privacy & Security > Microphone, and then Speech Recognition, and switch Pong on in each."
        }
    }
}

/// Hearing and speaking on the Mac: macOS's on-device speech recognition (audio never leaves
/// the Mac) and the system voice. An utterance ends after a 1.2 s pause or when macOS calls
/// its result final.
@MainActor
public final class SystemVoiceIO: NSObject, VoiceIO, AVSpeechSynthesizerDelegate {
    private let synth = AVSpeechSynthesizer()
    private var spoken: CheckedContinuation<Void, Never>?
    private let audio = AVAudioEngine()
    private let recognizer = SFSpeechRecognizer()
    /// Appended to from the audio thread; replaced only on the main actor.
    nonisolated(unsafe) private var request: SFSpeechAudioBufferRecognitionRequest?
    private var task: SFSpeechRecognitionTask?
    private var generation = 0
    private var latest = ""
    private var pause: Task<Void, Never>?
    private var failures = 0
    private var onHeard: (@MainActor (Heard) -> Void)?

    override public init() {
        super.init()
        synth.delegate = self
    }

    public func speak(_ text: String) async {
        finishSpeaking()
        await withCheckedContinuation { c in
            spoken = c
            synth.speak(AVSpeechUtterance(string: text))
        }
    }

    public func stopSpeaking() {
        synth.stopSpeaking(at: .immediate)
        finishSpeaking()
    }

    private func finishSpeaking() {
        spoken?.resume()
        spoken = nil
    }

    nonisolated public func speechSynthesizer(_ s: AVSpeechSynthesizer, didFinish u: AVSpeechUtterance) {
        Task { @MainActor in self.finishSpeaking() }
    }

    nonisolated public func speechSynthesizer(_ s: AVSpeechSynthesizer, didCancel u: AVSpeechUtterance) {
        Task { @MainActor in self.finishSpeaking() }
    }

    public func listen(_ onHeard: @escaping @MainActor (Heard) -> Void) {
        self.onHeard = onHeard
        guard let r = recognizer, r.isAvailable, r.supportsOnDeviceRecognition else {
            onHeard(.failed("on-device recognition is not available for this language"))
            return
        }
        let input = audio.inputNode
        input.installTap(onBus: 0, bufferSize: 1024, format: input.outputFormat(forBus: 0)) { [weak self] buffer, _ in
            guard let self else { return }
            self.request?.append(buffer)
            let l = Self.level(buffer)
            Task { @MainActor in self.onHeard?(.level(l)) }
        }
        audio.prepare()
        do {
            try audio.start()
        } catch {
            input.removeTap(onBus: 0)
            onHeard(.failed(error.localizedDescription))
            return
        }
        failures = 0
        begin()
    }

    public func stopListening() {
        onHeard = nil
        pause?.cancel()
        endTask()
        if audio.isRunning { audio.stop() }
        audio.inputNode.removeTap(onBus: 0)
    }

    private func begin() {
        guard let recognizer else { return }
        let req = SFSpeechAudioBufferRecognitionRequest()
        req.requiresOnDeviceRecognition = true
        req.shouldReportPartialResults = true
        request = req
        latest = ""
        generation += 1
        let g = generation
        task = recognizer.recognitionTask(with: req) { [weak self] result, error in
            let text = result?.bestTranscription.formattedString
            let final = result?.isFinal ?? false
            let failed = error?.localizedDescription
            Task { @MainActor in self?.recognized(text, final: final, error: failed, generation: g) }
        }
    }

    private func endTask() {
        generation += 1
        task?.cancel()
        task = nil
        request?.endAudio()
        request = nil
    }

    private func recognized(_ text: String?, final: Bool, error: String?, generation g: Int) {
        guard g == generation, onHeard != nil else { return }
        if let text, !text.isEmpty {
            failures = 0
            latest = text
            onHeard?(.partial(text))
            pause?.cancel()
            pause = Task {
                try? await Task.sleep(for: .milliseconds(1200))
                if !Task.isCancelled { self.utteranceEnded() }
            }
        }
        if final {
            utteranceEnded()
        } else if let error {
            // macOS ends a request after a stretch of silence; start another, unless it keeps failing.
            failures += 1
            if failures > 3 { onHeard?(.failed(error)); return }
            utteranceEnded()
        }
    }

    private func utteranceEnded() {
        pause?.cancel()
        let words = latest
        endTask()
        begin()
        if !words.isEmpty { onHeard?(.words(words)) }
    }

    /// The buffer's loudness, from -50 dB (0) to 0 dB (1).
    nonisolated static func level(_ b: AVAudioPCMBuffer) -> Float {
        guard let ch = b.floatChannelData?[0], b.frameLength > 0 else { return 0 }
        var sum: Float = 0
        for i in 0..<Int(b.frameLength) { sum += ch[i] * ch[i] }
        let rms = (sum / Float(b.frameLength)).squareRoot()
        let db = 20 * log10(max(rms, 1e-6))
        return min(max((db + 50) / 50, 0), 1)
    }
}
