import Foundation
import Observation

// Settings: the window's model. It shows what the Engine reports and passes each
// change on; the Engine checks it and keeps it.


/// `GET /settings`.
public struct EngineSettings: Decodable, Equatable, Sendable {
    public var cardCorner: CardCorner
    public var pingSound: PingSoundMode
    public var voice: Bool
    public var voiceAutoListen: Bool
    public var voiceModel: VoiceModelChoice
    public var voiceEndpoint: String?
    public var voiceEndpointModel: String?
    /// The card moves to the display with the focused window; off, it stays on the main display.
    public var cardFollowsActiveDisplay: Bool
    public var firstLaunchDone: Bool
    public var toldLoginDisallowed: Bool
    public var palette: [String]
}

/// The body of `PATCH /settings`; absent parts stay.
public struct SettingsChange: Encodable, Sendable {
    public var cardCorner: CardCorner?
    public var pingSound: PingSoundMode?
    public var voice: Bool?
    public var voiceAutoListen: Bool?
    public var voiceModel: VoiceModelChoice?
    public var voiceEndpoint: String?
    public var voiceEndpointModel: String?
    public var cardFollowsActiveDisplay: Bool?
    public var firstLaunchDone: Bool?
    public var toldLoginDisallowed: Bool?

    public init(cardCorner: CardCorner? = nil, cardFollowsActiveDisplay: Bool? = nil, pingSound: PingSoundMode? = nil, voice: Bool? = nil, voiceAutoListen: Bool? = nil,
                voiceModel: VoiceModelChoice? = nil, voiceEndpoint: String? = nil, voiceEndpointModel: String? = nil,
                firstLaunchDone: Bool? = nil, toldLoginDisallowed: Bool? = nil) {
        self.voiceModel = voiceModel
        self.voiceEndpoint = voiceEndpoint
        self.voiceEndpointModel = voiceEndpointModel
        self.cardCorner = cardCorner
        self.pingSound = pingSound
        self.voice = voice
        self.voiceAutoListen = voiceAutoListen
        self.cardFollowsActiveDisplay = cardFollowsActiveDisplay
        self.firstLaunchDone = firstLaunchDone
        self.toldLoginDisallowed = toldLoginDisallowed
    }
}

/// One Harness as `GET /harnesses` reports it.
public struct HarnessStatus: Decodable, Equatable, Sendable, Identifiable {
    public var id: String
    public var name: String
    public var installed: Bool
    public var connected: Bool
    public var version: String?
    /// An untested version or a manual step still outstanding, with what to do.
    public var warning: String?
    public var note: String?
    /// Where the human works in this Harness, "app" or "terminal"; only on a Harness that can
    /// move a Session started in a terminal into its own app.
    public var surface: String?
}

/// Where a human works in a Harness. Pong always starts a Session in the Harness's command-line
/// tool, where it runs at once; for someone who works in the app it has no window and is moved
/// into the app once its first turn ends.
public enum HarnessSurface: String, CaseIterable, Sendable {
    case app, terminal

    public var title: String {
        switch self {
        case .app: "Its app"
        case .terminal: "A terminal"
        }
    }
}

/// The parts of the Engine's API the Settings window uses.
public protocol SettingsAPI: Sendable {
    func settings() async throws -> EngineSettings
    func changeSettings(_ change: SettingsChange) async throws -> EngineSettings
    func harnesses() async throws -> [HarnessStatus]
    func repairHarness(_ id: String) async throws -> HarnessStatus
    /// Sets where the human works in the Harness.
    func setSurface(_ id: String, _ surface: HarnessSurface) async throws
    /// The voice model's API key from the Engine's Keychain, or nil when none is stored.
    func voiceKey() async throws -> String?
    /// Stores the key in the Engine's Keychain; nil clears it.
    func setVoiceKey(_ key: String?) async throws
}

extension EngineClient: SettingsAPI {
    private static let encoder: JSONEncoder = {
        let e = JSONEncoder()
        e.keyEncodingStrategy = .convertToSnakeCase
        return e
    }()
    /// `GET /harnesses` runs each Harness's own command, and repair reinstalls.
    private static let slow: TimeInterval = 120

    func json<T: Decodable>(_ path: String, _ method: String = "GET", _ body: Data? = nil, timeout: TimeInterval = 10) async throws -> T {
        try JSON.decoder.decode(T.self, from: await send(request(path, method: method, body: body), timeout: timeout))
    }

    public func settings() async throws -> EngineSettings { try await json("/settings") }
    public func changeSettings(_ change: SettingsChange) async throws -> EngineSettings {
        try await json("/settings", "PATCH", Self.encoder.encode(change))
    }
    public func harnesses() async throws -> [HarnessStatus] { try await json("/harnesses", timeout: Self.slow) }
    public func repairHarness(_ id: String) async throws -> HarnessStatus {
        try await json("/harnesses/\(id)/repair", "POST", timeout: Self.slow)
    }
    public func setSurface(_ id: String, _ surface: HarnessSurface) async throws {
        _ = try await send(request("/harnesses/\(id)/surface", method: "PUT", body: JSONEncoder().encode(["surface": surface.rawValue])))
    }
    public func voiceKey() async throws -> String? {
        struct Key: Decodable { var key: String }
        do {
            let k: Key = try await json("/voice/key")
            return k.key
        } catch let f as Failure where f.status == 404 {
            return nil
        }
    }
    public func setVoiceKey(_ key: String?) async throws {
        if let key {
            _ = try await send(request("/voice/key", method: "PUT", body: JSONEncoder().encode(["key": key])))
        } else {
            _ = try await send(request("/voice/key", method: "DELETE"))
        }
    }
}

/// What the Settings window shows, loaded from the Engine and macOS.
@MainActor @Observable
public final class SettingsModel {
    public private(set) var corner: CardCorner = .topRight
    public private(set) var pingSound: PingSoundMode = .first
    /// Voice mode, and listening as a Ping appears.
    public private(set) var voice = false
    public private(set) var voiceAutoListen = false
    /// The model that understands speech, and the endpoint's URL and model name.
    public private(set) var voiceModel: VoiceModelChoice = .none
    public private(set) var voiceEndpoint = ""
    public private(set) var voiceEndpointModel = ""
    /// An Anthropic API key is stored in the Keychain.
    public private(set) var voiceKeySet = false
    /// What the last "Test" found, and whether one is running.
    public private(set) var voiceTest: String?
    public private(set) var voiceTesting = false
    /// Why voice could not be turned on (a permission macOS was refused), with how to grant it.
    public private(set) var voiceProblem: String?
    public private(set) var followsActiveDisplay = false
    /// Nil while the Engine is still checking them (about a second).
    public private(set) var harnesses: [HarnessStatus]?
    public private(set) var repairing: Set<String> = []
    public private(set) var loginStatus: LoginItemStatus
    /// The last thing that went wrong, as the Engine or macOS said it.
    public private(set) var problem: String?

    @ObservationIgnored private let api: SettingsAPI
    @ObservationIgnored private let login: LoginItem
    @ObservationIgnored private let permissions: VoicePermissions
    @ObservationIgnored private let makeModel: (VoiceModelChoice, String?, String?, String?) -> VoiceModel?
    @ObservationIgnored private let account: (any PongAccount)?

    public init(api: SettingsAPI, login: LoginItem, permissions: VoicePermissions = SystemVoicePermissions(),
                account: (any PongAccount)? = Extras.current.account,
                makeModel: @escaping (VoiceModelChoice, String?, String?, String?) -> VoiceModel? = VoiceModels.make) {
        self.makeModel = makeModel
        self.account = account
        self.api = api
        self.login = login
        self.permissions = permissions
        loginStatus = login.status
    }

    public var startsAtLogin: Bool { loginStatus == .enabled }

    /// Loads everything; the settings come first, the slow Harness check after.
    public func load() async {
        loginStatus = login.status
        await attempt {
            let s = try await api.settings()
            corner = s.cardCorner
            pingSound = s.pingSound
            voice = s.voice
            voiceAutoListen = s.voiceAutoListen
            followsActiveDisplay = s.cardFollowsActiveDisplay
            apply(s)
            voiceKeySet = try await api.voiceKey() != nil
        }
        await loadHarnesses()
    }

    public func loadHarnesses() async {
        await attempt { harnesses = try await api.harnesses() }
    }

    public func setCorner(_ c: CardCorner) async {
        await attempt { corner = try await api.changeSettings(SettingsChange(cardCorner: c)).cardCorner }
    }

    public func setPingSound(_ m: PingSoundMode) async {
        await attempt { pingSound = try await api.changeSettings(SettingsChange(pingSound: m)).pingSound }
    }

    /// Turning voice on asks macOS for the microphone and speech recognition first (it asks the
    /// human only the first time); if either is refused, voice stays off and Settings says why.
    public func setVoice(_ on: Bool) async {
        if on {
            if let refused = await permissions.request() {
                voiceProblem = refused
                return
            }
        }
        voiceProblem = nil
        await attempt { voice = try await api.changeSettings(SettingsChange(voice: on)).voice }
    }

    public func setVoiceAutoListen(_ on: Bool) async {
        await attempt { voiceAutoListen = try await api.changeSettings(SettingsChange(voiceAutoListen: on)).voiceAutoListen }
    }

    private func apply(_ s: EngineSettings) {
        voiceModel = s.voiceModel
        voiceEndpoint = s.voiceEndpoint ?? ""
        voiceEndpointModel = s.voiceEndpointModel ?? ""
    }

    public func setVoiceModel(_ m: VoiceModelChoice) async {
        voiceTest = nil
        // Hosted is offered only by a build with an account; when the account may not use it,
        // the upgrade page opens and the previous choice stays.
        if m == .hosted {
            guard let account else { return }
            if !(await account.entitled()) {
                account.openUpgrade()
                voiceTest = Extras.hostedVoiceUpgrade
                return
            }
        }
        await attempt { apply(try await api.changeSettings(SettingsChange(voiceModel: m))) }
    }

    public func setVoiceEndpoint(_ url: String, model: String) async {
        voiceTest = nil
        await attempt { apply(try await api.changeSettings(SettingsChange(voiceEndpoint: url, voiceEndpointModel: model))) }
    }

    /// Stores the key in the Keychain, through the Engine; nil or empty clears it.
    public func setVoiceKey(_ key: String?) async {
        voiceTest = nil
        let k = key?.trimmingCharacters(in: .whitespacesAndNewlines)
        await attempt {
            try await api.setVoiceKey(k?.isEmpty == false ? k : nil)
            voiceKeySet = k?.isEmpty == false
        }
    }

    /// Sends a sample Ping to the chosen model and shows what it understood, or what failed.
    public func testVoiceModel() async {
        voiceTesting = true
        defer { voiceTesting = false }
        let key: String?? = switch voiceModel {
        case .anthropic: try? await api.voiceKey()
        case .hosted: await account?.token()
        default: nil
        }
        guard let model = makeModel(voiceModel, voiceEndpoint, voiceEndpointModel, key ?? nil) else {
            voiceTest = voiceModel == .none ? "Choose a model to test." : "Set the model up first."
            return
        }
        do {
            let intent = try await VoiceUnderstanding.understand(VoiceSample.transcript, VoiceSample.ping, model: model)
            voiceTest = VoiceSample.describe(intent)
        } catch {
            voiceTest = "Failed: \(error.localizedDescription)"
        }
    }

    public func setFollowsActiveDisplay(_ on: Bool) async {
        await attempt {
            followsActiveDisplay = try await api.changeSettings(SettingsChange(cardFollowsActiveDisplay: on)).cardFollowsActiveDisplay
        }
    }


    public func setStartsAtLogin(_ on: Bool) {
        problem = nil
        do {
            if on { try login.register() } else { try login.unregister() }
        } catch {
            problem = "\(error.localizedDescription)"
        }
        loginStatus = login.status
    }

    public func repair(_ id: String) async {
        repairing.insert(id)
        defer { repairing.remove(id) }
        await attempt {
            let h = try await api.repairHarness(id)
            if let i = harnesses?.firstIndex(where: { $0.id == id }) { harnesses?[i] = h }
        }
    }

    /// Sets where the human works in the Harness; shown at once, and put back if the Engine refuses.
    public func setSurface(_ id: String, _ surface: HarnessSurface) async {
        guard let i = harnesses?.firstIndex(where: { $0.id == id }) else { return }
        let was = harnesses?[i].surface
        harnesses?[i].surface = surface.rawValue
        await attempt { try await api.setSurface(id, surface) }
        if problem != nil, let i = harnesses?.firstIndex(where: { $0.id == id }) { harnesses?[i].surface = was }
    }

    private func attempt(_ work: () async throws -> Void) async {
        do {
            try await work()
            problem = nil
        } catch {
            problem = "\(error)"
        }
    }
}

/// The first-launch flags, kept by the Engine so they survive reinstalling the app.
@MainActor
public final class EngineLaunchRecord: LaunchRecord {
    private let api: SettingsAPI
    public var firstLaunchDone: Bool {
        didSet { save(SettingsChange(firstLaunchDone: firstLaunchDone)) }
    }
    public var toldLoginDisallowed: Bool {
        didSet { save(SettingsChange(toldLoginDisallowed: toldLoginDisallowed)) }
    }

    private init(api: SettingsAPI, settings: EngineSettings) {
        self.api = api
        firstLaunchDone = settings.firstLaunchDone
        toldLoginDisallowed = settings.toldLoginDisallowed
    }

    /// The record as the Engine keeps it.
    public static func load(_ api: SettingsAPI) async throws -> EngineLaunchRecord {
        EngineLaunchRecord(api: api, settings: try await api.settings())
    }

    /// Writes made through this record, so a caller can wait for them.
    public private(set) var saving: [Task<Void, Never>] = []

    private func save(_ change: SettingsChange) {
        let api = api
        saving.append(Task { _ = try? await api.changeSettings(change) })
    }
}
