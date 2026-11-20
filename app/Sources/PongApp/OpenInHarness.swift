import AppKit
import PongKit
import UniformTypeIdentifiers

/// Opens a terminal by writing the command into a `.command` file and opening it with Launch
/// Services (Terminal by default, or whichever app the human set for `.command` files). No Apple
/// Events, so no Automation prompt; the file is ours and not quarantined, so no Gatekeeper prompt.
@MainActor
struct SystemHarnessOpener: HarnessOpener {
    func openTerminal(running command: String) throws {
        let dir = FileManager.default.temporaryDirectory.appendingPathComponent("pong-resume", isDirectory: true)
        try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        let file = dir.appendingPathComponent("\(UUID().uuidString).command")
        // Run in the login shell so `claude` is on the PATH, then stay in an interactive shell.
        let script = "#!/bin/sh\nrm -f \"$0\"\nexec \"${SHELL:-/bin/zsh}\" -l -c \(shellQuote(command + "; exec \"${SHELL:-/bin/zsh}\" -l"))\n"
        try script.write(to: file, atomically: true, encoding: .utf8)
        try FileManager.default.setAttributes([.posixPermissions: 0o755], ofItemAtPath: file.path)
        guard NSWorkspace.shared.open(file) else {
            throw CocoaError(.fileReadUnknown, userInfo: [NSFilePathErrorKey: file.path])
        }
    }

    /// Runs the command in the login shell, so `claude` is on the PATH, under `script`, which gives
    /// it the terminal `claude --desktop` insists on.
    func openApp(running command: String) throws {
        let p = Process()
        p.executableURL = URL(fileURLWithPath: "/usr/bin/script")
        p.arguments = ["-q", "/dev/null", ProcessInfo.processInfo.environment["SHELL"] ?? "/bin/zsh", "-l", "-c", command]
        p.standardInput = FileHandle.nullDevice
        p.standardOutput = FileHandle.nullDevice
        p.standardError = FileHandle.nullDevice
        try p.run()
        p.waitUntilExit()
        guard p.terminationStatus == 0 else { throw CocoaError(.executableLoad) }
    }

    /// What the open button's destination is called. A terminal is whichever app opens
    /// `.command` files here.
    func opensIn(_ target: OpenTarget?) -> String? {
        switch target {
        case .app(let name): name
        case .terminal:
            UTType(filenameExtension: "command").flatMap { NSWorkspace.shared.urlForApplication(toOpen: $0) }?
                .deletingPathExtension().lastPathComponent
        case nil: nil
        }
    }

    func copy(_ text: String) {
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(text, forType: .string)
    }

    private func shellQuote(_ s: String) -> String { "'" + s.replacingOccurrences(of: "'", with: "'\\''") + "'" }
}
