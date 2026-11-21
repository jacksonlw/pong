import Carbon.HIToolbox
import PongKit

/// Registers the shortcuts set in Settings with macOS, so they work while another app is in front.
/// Carbon's hot keys need no Accessibility permission and take the key press from other apps.
@MainActor
final class HotKeys {
    private var refs: [EventHotKeyRef] = []
    private var handler: EventHandlerRef?
    private var actions: [UInt32: ShortcutAction] = [:]
    private let run: (ShortcutAction) -> Void

    init(run: @escaping (ShortcutAction) -> Void) {
        self.run = run
        var spec = EventTypeSpec(eventClass: OSType(kEventClassKeyboard), eventKind: UInt32(kEventHotKeyPressed))
        InstallEventHandler(GetApplicationEventTarget(), { _, event, me in
            var id = EventHotKeyID()
            GetEventParameter(event, EventParamName(kEventParamDirectObject), EventParamType(typeEventHotKeyID),
                              nil, MemoryLayout<EventHotKeyID>.size, nil, &id)
            let keys = Unmanaged<HotKeys>.fromOpaque(me!).takeUnretainedValue()
            MainActor.assumeIsolated { keys.pressed(id.id) }
            return noErr
        }, 1, &spec, Unmanaged.passUnretained(self).toOpaque(), &handler)
    }

    /// Replaces what is registered with these shortcuts.
    func register(_ shortcuts: [ShortcutAction: Shortcut]) {
        refs.forEach { UnregisterEventHotKey($0) }
        refs = []
        actions = [:]
        for (i, a) in ShortcutAction.allCases.enumerated() {
            guard let s = shortcuts[a] else { continue }
            let id = UInt32(i + 1)
            var ref: EventHotKeyRef?
            let status = RegisterEventHotKey(UInt32(s.keyCode), s.carbonModifiers,
                                             EventHotKeyID(signature: "Pong".utf8.reduce(0) { $0 << 8 | OSType($1) }, id: id),
                                             GetApplicationEventTarget(), 0, &ref)
            guard status == noErr, let ref else {
                print("shortcut \(s.title) for \(a.rawValue) not registered: \(status)")
                continue
            }
            refs.append(ref)
            actions[id] = a
        }
    }

    private func pressed(_ id: UInt32) {
        guard let a = actions[id] else { return }
        print("shortcut \(a.rawValue) pressed")
        run(a)
    }
}
