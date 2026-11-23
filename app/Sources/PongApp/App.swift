import AppKit
import AudioToolbox
import PongKit


/// Where the Engine keeps its data. `PONG_HOME` overrides; the real data directory is used
/// only when running as the real bundle, and a fresh temporary one otherwise.
func dataDirectory() -> (URL, isReal: Bool) {
    if let d = ProcessInfo.processInfo.environment["PONG_HOME"], !d.isEmpty {
        return (URL(fileURLWithPath: d), false)
    }
    if Bundle.main.bundleURL.pathExtension == "app" {
        let support = FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask)[0]
        return (support.appendingPathComponent("Pong"), true)
    }
    let tmp = FileManager.default.temporaryDirectory.appendingPathComponent("pong-dev-\(UUID().uuidString)")
    return (tmp, false)
}

/// The Engine inside the bundle (Contents/Helpers, beside the `pong` tool; not in MacOS,
/// where `pong` and `Pong` would collide on a case-insensitive disk), or `PONG_ENGINE`.
func engineExecutable() -> URL {
    if let e = ProcessInfo.processInfo.environment["PONG_ENGINE"], !e.isEmpty {
        return URL(fileURLWithPath: e)
    }
    return Bundle.main.bundleURL.appendingPathComponent("Contents/Helpers/pong-engine")
}

@MainActor
final class AppDelegate: NSObject, NSApplicationDelegate {
    private var engine: EngineHost!
    private let inbox = Inbox()
    private var card: CardController!
    private var following: Task<Void, Never>?
    private var background: Background?
    private var previews: PreviewController!
    /// The row of Threads, Settings and previews beside the card.
    private let dock = Dock()
    private var settings: SettingsWindowController!
    private var newSessionSignal: DispatchSourceSignal?
    private var rightClicks: Any?
    private var statusItem: StatusItem?
    private var zoomKeys: Any?
    private var zoomControl: ZoomControlWindow?
    private var threads: ThreadWindowController!
    private var joins: JoinWindowController!
    private let arrivals = PingArrivals()
    private var soundIDs: [PingSound: SystemSoundID] = [:]

    func applicationDidFinishLaunching(_ notification: Notification) {
        let (dir, isReal) = dataDirectory()
        engine = isReal
            ? EngineService(pong: Bundle.main.bundleURL.appendingPathComponent("Contents/Helpers/pong"), dir: dir)
            : EngineProcess(executable: engineExecutable(), dir: dir, extraArgs: ["-port", "0"])
        engine.start()

        let engine = engine!
        let shortcuts = ShortcutStore()
        settings = SettingsWindowController(client: { await engine.client() },
                                            login: isReal ? MainAppLoginItem() : PretendLoginItem(), dock: dock,
                                            shortcuts: shortcuts)
        let settings = settings!
        NSApp.mainMenu = mainMenu(settings: { settings.show() })
        // A right click anywhere in Pong's windows leads to Settings and Quit too.
        rightClicks = PongMenu.watch(settings: { settings.show() })
        // ⌘+ and ⌘− (or ⌃) zoom everything in Pong's windows, from whichever of them has focus.
        zoomKeys = NSEvent.addLocalMonitorForEvents(matching: .keyDown) { e in
            MainActor.assumeIsolated { Zoom.shared.handle(e) } ? nil : e
        }
        zoomControl = ZoomControlWindow() // shows the zoom above the Bar as it changes
        SharedSkills.names = SkillNames(fetch: { place in try? await engine.client()?.skills(place) })
        // The Bar is Pong's place on screen: it starts Sessions and leads to Settings and Quit.
        card = CardController(inbox: inbox, directory: CardContext.live.directory, client: { await engine.client() },
                              settings: { settings.show() }, openThread: { [weak self] in self?.threads.show($0) })
        let dock = dock
        card.dropped = { dock.cardDropped($0, settled: $1) }
        statusItem = StatusItem(card: card, directory: CardContext.live.directory,
                                client: { await engine.client() }, settings: { settings.show() },
                                shortcuts: shortcuts)
        if !isReal { // live checks: SIGUSR1 presses the Bar's New Session button
            signal(SIGUSR1, SIG_IGN)
            let press = DispatchSource.makeSignalSource(signal: SIGUSR1, queue: .main)
            press.setEventHandler { [weak self] in self?.card.newSession() }
            press.resume()
            newSessionSignal = press
        }
        // Voice's model, as Settings chose it; an Anthropic key is read from the Engine's Keychain
        // each time, and hosted voice's Account token from the Mac's Keychain.
        card.voice.model = {
            let d = CardContext.live.directory
            let key: String?? = switch d.voiceModel {
            case .anthropic: try? await engine.client()?.voiceKey()
            case .hosted: await Extras.current.account?.token()
            default: nil
            }
            return VoiceModels.make(d.voiceModel, endpoint: d.voiceEndpoint, endpointModel: d.voiceEndpointModel, key: key ?? nil)
        }
        threads = ThreadWindowController(client: { await engine.client() }, dock: dock, showInQueue: { [inbox] in inbox.show($0.id) },
                                         changed: { [weak self] in self?.card.set(openThreads: $0) })
        previews = PreviewController(inbox: inbox, dock: dock, client: { await engine.client() })
        joins = JoinWindowController(client: { await engine.client() }, dock: dock)
        joins.start()
        following = Task { await inbox.follow(clientProvider: { await engine.client() }) }
        ContextWindowController.start(clientProvider: { await engine.client() })
        if isReal { background = Background(inbox: inbox, client: { await engine.client() }) } // never under PONG_HOME or tests
        trackPings()
    }

    /// On every change to the waiting Pings: lock-screen notifications, and the sound of any
    /// Ping that just arrived, played with its notification when it gets one, else here.
    private func trackPings() {
        withObservationTracking { _ = inbox.waiting } onChange: { [weak self] in
            DispatchQueue.main.async { self?.pingsChanged(); self?.trackPings() }
        }
    }

    private func pingsChanged() {
        let sounds = arrivals.sounds(inbox.waiting, mode: CardContext.live.directory.pingSound)
        let notified = background?.update(sounds: sounds) ?? []
        guard let sound = PingSound.one(of: sounds.filter { !notified.contains($0.key) }.values) else { return }
        play(sound)
    }

    /// A system sound plays at the system alert volume.
    private func play(_ sound: PingSound) {
        if soundIDs[sound] == nil, let url = sound.url {
            var id: SystemSoundID = 0
            if AudioServicesCreateSystemSoundID(url as CFURL, &id) == noErr { soundIDs[sound] = id }
        }
        if let id = soundIDs[sound] { AudioServicesPlaySystemSound(id) }
    }

    func applicationWillTerminate(_ notification: Notification) {
        following?.cancel()
        joins.stop()
        engine.stop()
    }
}


/// Runs the Mac app with what a build adds beyond Local, and never returns. Public pong's Pong
/// passes no extras.
@MainActor public func runPong(_ extras: PongExtras = PongExtras()) -> Never {
    Extras.current = extras
    setvbuf(stdout, nil, _IOLBF, 0)
    registerBrandFont()
    let app = NSApplication.shared
    app.setActivationPolicy(.accessory)
    let delegate = MainActor.assumeIsolated { AppDelegate() }
    app.delegate = delegate
    signal(SIGTERM, SIG_IGN)
    let sigterm = DispatchSource.makeSignalSource(signal: SIGTERM, queue: .main)
    sigterm.setEventHandler { NSApp.terminate(nil) }
    sigterm.resume()
    app.run()
    exit(0)
}
