import Foundation
import PongKit

/// Runs the Engine as a child process, restarts it if it dies, and stops it on quit. Used only
/// under `PONG_HOME` and in tests; the real app uses `EngineService`.
final class EngineProcess: EngineHost, @unchecked Sendable {
    let executable: URL
    let dir: URL
    /// Arguments besides `-dir`; empty for the real bundle (the Engine's own default port).
    let extraArgs: [String]
    private let lock = NSLock()
    private var process: Process?
    private var stopping = false

    init(executable: URL, dir: URL, extraArgs: [String]) {
        self.executable = executable
        self.dir = dir
        self.extraArgs = extraArgs
    }

    func start() {
        lock.lock(); defer { lock.unlock() }
        guard !stopping else { return }
        let p = Process()
        p.executableURL = executable
        p.arguments = ["-dir", dir.path, "-exit-with-stdin"] + extraArgs
        p.standardInput = Pipe() // held by this process: the Engine exits when it dies
        p.terminationHandler = { [weak self] p in
            print("Engine exited (status \(p.terminationStatus))")
            DispatchQueue.global().asyncAfter(deadline: .now() + 1) { self?.start() }
        }
        do {
            try p.run()
            process = p
            print("Engine started, pid \(p.processIdentifier), dir \(dir.path)")
        } catch {
            print("Engine failed to start: \(error)")
            DispatchQueue.global().asyncAfter(deadline: .now() + 1) { [weak self] in self?.start() }
        }
    }

    func stop() {
        lock.lock()
        stopping = true
        let p = process
        lock.unlock()
        guard let p, p.isRunning else { return }
        p.terminationHandler = nil
        p.terminate()
        p.waitUntilExit()
    }

    /// A client for the running Engine, once it answers.
    func client() async -> EngineClient? {
        for _ in 0..<50 {
            if let c = try? EngineClient.load(dir: dir), (try? await c.health()) != nil {
                return c
            }
            try? await Task.sleep(for: .milliseconds(100))
        }
        return nil
    }
}

/// The Engine as a background service: a launch agent runs it whether or not the app is
/// open. The app asks `pong service start` to start it, which installs the agent when it is missing,
/// and then only connects; quitting the app leaves the Engine and its Sessions running.
final class EngineService: EngineHost, @unchecked Sendable {
    let pong: URL
    let dir: URL

    init(pong: URL, dir: URL) {
        self.pong = pong
        self.dir = dir
    }

    func start() {
        DispatchQueue.global().async { [pong] in
            let p = Process()
            p.executableURL = pong
            p.arguments = ["service", "start"]
            do {
                try p.run()
                p.waitUntilExit()
                print("pong service start exited \(p.terminationStatus)")
            } catch {
                print("pong service start failed: \(error)")
            }
        }
    }

    func stop() {}

    func client() async -> EngineClient? {
        for _ in 0..<100 {
            if let c = try? EngineClient.load(dir: dir), (try? await c.health()) != nil {
                return c
            }
            try? await Task.sleep(for: .milliseconds(100))
        }
        return nil
    }
}

/// What the app needs from whatever runs its Engine.
protocol EngineHost: AnyObject, Sendable {
    func start()
    func stop()
    func client() async -> EngineClient?
}
