import AppKit
import Carbon.HIToolbox
import SwiftUI

/// A key with modifiers that works from any app: the human sets one in Settings for each `ShortcutAction`.
public struct Shortcut: Codable, Equatable, Sendable {
    /// The key's virtual key code, as `NSEvent.keyCode` gives it.
    public var keyCode: UInt16
    /// `NSEvent.ModifierFlags` raw value, holding only Command, Option, Control and Shift.
    public var modifiers: UInt
    /// What the key types with no modifiers, shown in Settings and the menu.
    public var key: String

    public init(keyCode: UInt16, modifiers: NSEvent.ModifierFlags, key: String) {
        self.keyCode = keyCode
        self.modifiers = modifiers.intersection(Self.allowed).rawValue
        self.key = key
    }

    static let allowed: NSEvent.ModifierFlags = [.command, .option, .control, .shift]

    public var flags: NSEvent.ModifierFlags { NSEvent.ModifierFlags(rawValue: modifiers) }

    /// The key press as a shortcut, or nil when it cannot be one: a key from any app needs Command,
    /// Option or Control, or it would take a letter the human is typing elsewhere.
    public init?(event e: NSEvent) {
        let mods = e.modifierFlags.intersection(Self.allowed)
        guard !mods.intersection([.command, .option, .control]).isEmpty,
              let key = Self.name(keyCode: e.keyCode, typed: e.charactersIgnoringModifiers) else { return nil }
        self.init(keyCode: e.keyCode, modifiers: mods, key: key)
    }

    /// How the shortcut reads, in the Mac's order: ⌃⌥⇧⌘ then the key.
    public var title: String {
        var s = ""
        if flags.contains(.control) { s += "⌃" }
        if flags.contains(.option) { s += "⌥" }
        if flags.contains(.shift) { s += "⇧" }
        if flags.contains(.command) { s += "⌘" }
        return s + key
    }

    /// The modifiers as Carbon's hot key API takes them.
    public var carbonModifiers: UInt32 {
        var m: UInt32 = 0
        if flags.contains(.command) { m |= UInt32(cmdKey) }
        if flags.contains(.option) { m |= UInt32(optionKey) }
        if flags.contains(.control) { m |= UInt32(controlKey) }
        if flags.contains(.shift) { m |= UInt32(shiftKey) }
        return m
    }

    private static let named: [Int: String] = [
        kVK_Space: "Space", kVK_Return: "↩", kVK_Tab: "⇥", kVK_Delete: "⌫", kVK_ForwardDelete: "⌦",
        kVK_LeftArrow: "←", kVK_RightArrow: "→", kVK_UpArrow: "↑", kVK_DownArrow: "↓",
        kVK_Home: "↖", kVK_End: "↘", kVK_PageUp: "⇞", kVK_PageDown: "⇟",
        kVK_F1: "F1", kVK_F2: "F2", kVK_F3: "F3", kVK_F4: "F4", kVK_F5: "F5", kVK_F6: "F6",
        kVK_F7: "F7", kVK_F8: "F8", kVK_F9: "F9", kVK_F10: "F10", kVK_F11: "F11", kVK_F12: "F12",
    ]

    static func name(keyCode: UInt16, typed: String?) -> String? {
        if let n = named[Int(keyCode)] { return n }
        guard let t = typed?.trimmingCharacters(in: .whitespacesAndNewlines), !t.isEmpty,
              t.unicodeScalars.allSatisfy({ !CharacterSet.controlCharacters.contains($0) }) else { return nil }
        return t.uppercased()
    }
}

/// What a shortcut does.
public enum ShortcutAction: String, CaseIterable, Sendable {
    case toggleShown, toggleMuted, speak

    public var title: String {
        switch self {
        case .toggleShown: "Show or hide Pong"
        case .toggleMuted: "Mute Ping sounds"
        case .speak: "Speak into Pong"
        }
    }

    /// The shortcut an action has until the human sets or clears one.
    public var standard: Shortcut? {
        switch self {
        case .speak: Shortcut(keyCode: UInt16(kVK_Space), modifiers: [.option, .command], key: "Space")
        default: nil
        }
    }
}

/// The shortcuts this Mac has set, kept in its defaults. `changed` runs on every change so the app
/// registers them again.
@MainActor @Observable
public final class ShortcutStore {
    @ObservationIgnored private let defaults: UserDefaults
    @ObservationIgnored public var changed: () -> Void = {}
    public private(set) var shortcuts: [ShortcutAction: Shortcut] = [:]

    public init(defaults: UserDefaults = .standard) {
        self.defaults = defaults
        for a in ShortcutAction.allCases {
            if let data = defaults.data(forKey: Self.key(a)), let s = try? JSONDecoder().decode(Shortcut.self, from: data) {
                shortcuts[a] = s
            } else if !defaults.bool(forKey: Self.clearedKey(a)), let s = a.standard, !shortcuts.values.contains(s) {
                shortcuts[a] = s
            }
        }
    }

    static func key(_ a: ShortcutAction) -> String { "shortcut.\(a.rawValue)" }
    /// Set once the human clears a shortcut, so its standard one does not come back.
    static func clearedKey(_ a: ShortcutAction) -> String { "shortcut.\(a.rawValue).cleared" }

    public subscript(_ a: ShortcutAction) -> Shortcut? { shortcuts[a] }

    /// Sets or clears an action's shortcut. One that another action has moves to this one.
    public func set(_ s: Shortcut?, for a: ShortcutAction) {
        if let s {
            for (other, held) in shortcuts where other != a && held == s { store(nil, for: other) }
        }
        store(s, for: a)
        changed()
    }

    private func store(_ s: Shortcut?, for a: ShortcutAction) {
        shortcuts[a] = s
        if let s, let data = try? JSONEncoder().encode(s) {
            defaults.set(data, forKey: Self.key(a))
            defaults.removeObject(forKey: Self.clearedKey(a))
        } else {
            defaults.removeObject(forKey: Self.key(a))
            defaults.set(true, forKey: Self.clearedKey(a))
        }
    }
}

/// A row in Settings that records a shortcut: click it, press the keys. Escape stops recording and
/// Delete clears the shortcut.
struct ShortcutRecorder: View {
    let action: ShortcutAction
    let store: ShortcutStore
    @State private var recording = false
    @State private var monitor: Any?

    var body: some View {
        HStack {
            Text(action.title)
            Spacer()
            Button(recording ? "Press keys…" : (store[action]?.title ?? "Record shortcut")) {
                recording ? stop() : start()
            }
            .buttonStyle(BrandButtonStyle())
            .accessibilityLabel("\(action.title) shortcut")
            if store[action] != nil, !recording {
                Button("Clear") { store.set(nil, for: action) }
                    .buttonStyle(BrandButtonStyle())
                    .accessibilityLabel("Clear \(action.title) shortcut")
            }
        }
        .onDisappear { stop() }
    }

    private func start() {
        recording = true
        monitor = NSEvent.addLocalMonitorForEvents(matching: .keyDown) { e in
            MainActor.assumeIsolated {
                switch Int(e.keyCode) {
                case kVK_Escape where e.modifierFlags.intersection(Shortcut.allowed).isEmpty: stop()
                case kVK_Delete where e.modifierFlags.intersection(Shortcut.allowed).isEmpty:
                    store.set(nil, for: action)
                    stop()
                default:
                    guard let s = Shortcut(event: e) else { NSSound.beep(); return }
                    store.set(s, for: action)
                    stop()
                }
            }
            return nil
        }
    }

    private func stop() {
        recording = false
        if let monitor { NSEvent.removeMonitor(monitor) }
        monitor = nil
    }
}
