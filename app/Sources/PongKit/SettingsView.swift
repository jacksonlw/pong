import SwiftUI

/// The Settings window's content: the card's corner, the Ping sound, start at login and each Harness. Projects
/// are edited in the Project menu (`ProjectMenu`), not here.
public struct SettingsView: View {
    @Bindable var model: SettingsModel
    var devices: DevicesModel?

    public static let width: CGFloat = 560

    public init(model: SettingsModel, devices: DevicesModel? = nil) {
        self.model = model
        self.devices = devices
    }

    public var body: some View {
        VStack(alignment: .leading, spacing: 22) {
            SettingsLogo()
            if let problem = model.problem {
                Label(problem, systemImage: "exclamationmark.octagon").inlineIcon(.body)
                    .foregroundStyle(Brand.warning)
                    .fixedSize(horizontal: false, vertical: true)
            }
            section("Card") {
                HStack {
                    Text("Corner")
                    Spacer()
                    BrandSegments(options: [CardCorner.topLeft, .topRight, .bottomLeft, .bottomRight].map { ($0, $0.title) },
                                  selection: Binding(get: { model.corner }, set: { c in Task { await model.setCorner(c) } }))
                        .fixedSize()
                        .accessibilityLabel("Corner")
                }
                Toggle(isOn: Binding(get: { model.followsActiveDisplay },
                                     set: { on in Task { await model.setFollowsActiveDisplay(on) } })) {
                    Text("Move the card to the display you are working on")
                }
                .toggleStyle(BrandToggleStyle())
            }
            pingSoundSection
            voiceSection
            section("Start at login") {
                Toggle(isOn: Binding(get: { model.startsAtLogin }, set: { model.setStartsAtLogin($0) })) {
                    Text("Start Pong when you log in")
                }
                .toggleStyle(BrandToggleStyle())
                if model.loginStatus == .requiresApproval {
                    Text("macOS has not allowed this yet. Open System Settings > General > Login Items and switch Pong on.")
                        .brandStyle(.secondary)
                        .foregroundStyle(Brand.secondary)
                        .fixedSize(horizontal: false, vertical: true)
                }
            }
            if let devices {
                section("Devices") { DevicesSection(model: devices) }
                    .task { await devices.follow() }
            }
            section("Harnesses") {
                if let harnesses = model.harnesses {
                    ForEach(harnesses) { h in
                        HarnessRow(harness: h, repairing: model.repairing.contains(h.id),
                                   repair: { Task { await model.repair(h.id) } },
                                   setSurface: { s in Task { await model.setSurface(h.id, s) } })
                    }
                } else {
                    HStack(spacing: 8) {
                        ProgressView().controlSize(.small)
                        Text("Checking each Harness…").foregroundStyle(Brand.secondary)
                    }
                }
            }
        }
        .brandStyle(.body)
        .foregroundStyle(Brand.text)
        .padding(24)
        .frame(width: Self.width, alignment: .leading)
        .background(Brand.page)
    }

    private var pingSoundSection: some View {
    section("Ping sound", info: InfoButton(about: "Ping sound", text: "A follow-up is a Ping from a Session within five minutes of your last answer to it, and plays a softer sound. Notes make no sound.",
                                               learnMore: Docs.page("settings", "ping-sound"))) {
            HStack {
                Text("Play a sound")
                Spacer()
                BrandSegments(options: PingSoundMode.allCases.map { ($0, $0.title) },
                              selection: Binding(get: { model.pingSound }, set: { (m: PingSoundMode) in Task { await model.setPingSound(m) } }))
                    .fixedSize()
                    .accessibilityLabel("Ping sound")
            }
        }
    }

    private var voiceSection: some View {
        section("Voice", info: InfoButton(about: "Voice", text: "Speech is recognised on this Mac, and no audio leaves it. Without Listen when a Ping appears, start from the microphone on the card.",
                                          learnMore: Docs.page("settings", "voice"))) {
            Toggle(isOn: Binding(get: { model.voice }, set: { on in Task { await model.setVoice(on) } })) {
                Text("Read Pings aloud and answer by speaking")
            }
            .toggleStyle(BrandToggleStyle())
            if let problem = model.voiceProblem {
                Label { Text(problem).fixedSize(horizontal: false, vertical: true) } icon: { Image(systemName: "exclamationmark.triangle.fill").fontWeight(Brand.iconWeight) }
                .inlineIcon(.secondary)
                .brandStyle(.secondary)
                .foregroundStyle(Brand.warning)
            }
            Toggle(isOn: Binding(get: { model.voiceAutoListen }, set: { on in Task { await model.setVoiceAutoListen(on) } })) {
                Text("Listen when a Ping appears")
            }
            .toggleStyle(BrandToggleStyle())
            .disabled(!model.voice)
            VoiceModelSettings(model: model)
        }
    }

    private func section<Content: View>(_ title: String, info: InfoButton? = nil, @ViewBuilder content: () -> Content) -> some View {
        return VStack(alignment: .leading, spacing: 8) {
            HStack(alignment: .capMiddle, spacing: 6) {
                Text(title).brandStyle(.heading).capCentred(.heading)
                if let info { info }
            }
            VStack(alignment: .leading, spacing: 12) { content() }
                .padding(14)
                .frame(maxWidth: .infinity, alignment: .leading)
                .background(Brand.surface, in: Rectangle())
        }
    }
}

struct HarnessRow: View {
    var harness: HarnessStatus
    var repairing: Bool
    var repair: () -> Void
    var setSurface: (HarnessSurface) -> Void = { _ in }

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(alignment: .firstTextBaseline) {
                Text(harness.name).brandStyle(.answer)
                if let v = harness.version { Text(v).foregroundStyle(Brand.secondary) }
                Spacer()
                if repairing {
                    ProgressView().controlSize(.small)
                    Text("Repairing…").foregroundStyle(Brand.secondary)
                } else {
                    Button("Repair", action: repair).buttonStyle(.brand).disabled(!harness.installed)
                }
            }
            HStack(spacing: 14) {
                state(harness.installed, "Installed", "Not installed")
                state(harness.connected, "Connected", "Not connected")
            }
            if let surface = harness.surface.flatMap(HarnessSurface.init(rawValue:)) {
                HStack(alignment: .capMiddle) {
                    Text("You work in").capCentred(.body)
                    InfoButton(about: "where you work in \(harness.name)",
                               text: "Its app: Pong starts Sessions with no terminal window and moves them into the app once their first turn ends. A terminal: Pong starts Sessions in a terminal.",
                               learnMore: Docs.page("settings", "harnesses"))
                    Spacer()
                    BrandSegments(options: HarnessSurface.allCases.map { ($0, $0.title) },
                                  selection: Binding(get: { surface }, set: setSurface))
                        .fixedSize()
                        .accessibilityLabel("Where you work in \(harness.name)")
                }
                .padding(.top, 4)
            }
            if let w = harness.warning {
                Label { Text(w.replacingOccurrences(of: "/", with: "/\u{2060}")).fixedSize(horizontal: false, vertical: true) } icon: { Image(systemName: "exclamationmark.triangle.fill").fontWeight(Brand.iconWeight) }
                .inlineIcon(.body)
                .foregroundStyle(Brand.warning)
            }
        }
    }

    private func state(_ on: Bool, _ yes: String, _ no: String) -> some View {
        Label(on ? yes : no, systemImage: on ? "checkmark.circle.fill" : "circle").inlineIcon(.secondary)
            .brandStyle(.secondary)
            .foregroundStyle(on ? Brand.text : Brand.secondary)
    }
}

/// The model that understands speech: a build's hosted model, an OpenAI-compatible endpoint,
/// or the human's Anthropic API key, with a test.
struct VoiceModelSettings: View {
    @Bindable var model: SettingsModel
    @State private var url = ""
    @State private var name = ""
    @State private var key = ""

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack(alignment: .capMiddle) {
                Text("Model").capCentred(.body)
                InfoButton(about: "the voice model",
                           text: "Only the words you say and the Ping's question, answers and context go to a model; never audio. Without a model, say a number, \"recommended\", \"yes\" or \"no\". An Anthropic API key is kept in the macOS Keychain.",
                           learnMore: Docs.page("settings", "voice"))
                Spacer()
                BrandSegments(options: VoiceModelChoice.allCases.filter { $0 != .hosted || Extras.current.hostedVoice != nil }.map { ($0, $0.title) },
                              selection: Binding(get: { model.voiceModel }, set: { (m: VoiceModelChoice) in Task { await model.setVoiceModel(m) } }))
                    .fixedSize()
                    .accessibilityLabel("Voice model")
            }
            switch model.voiceModel {
            case .none:
                EmptyView()
            case .hosted:
                HStack {
                    Spacer()
                    testButton
                }
            case .endpoint:
                field("URL", "http://localhost:11434/v1", $url)
                field("Model name", "llama3.2", $name)
                HStack {
                    Spacer()
                    Button("Save") { Task { await model.setVoiceEndpoint(url, model: name) } }.buttonStyle(.brand)
                    testButton
                }
            case .anthropic:
                HStack(spacing: 8) {
                    SecureField("", text: $key)
                        .textFieldStyle(.plain)
                        .brandPlaceholder(model.voiceKeySet ? "A key is kept in the Keychain" : "sk-ant-…", shown: key.isEmpty)
                        .padding(.vertical, 6)
                        .padding(.horizontal, 10)
                        .background(Brand.row, in: Rectangle())
                        .accessibilityLabel("Anthropic API key")
                    Button("Save") { let k = key; key = ""; Task { await model.setVoiceKey(k) } }.buttonStyle(.brand).disabled(key.isEmpty)
                    if model.voiceKeySet { Button("Clear") { Task { await model.setVoiceKey(nil) } }.buttonStyle(.brand) }
                    testButton
                }
            }
            if let result = model.voiceTest {
                Text(result)
                    .brandStyle(.secondary)
                    .foregroundStyle(result.hasPrefix("Works") ? Brand.text : Brand.warning)
                    .fixedSize(horizontal: false, vertical: true)
            }
        }
        .onAppear { url = model.voiceEndpoint; name = model.voiceEndpointModel }
        .onChange(of: model.voiceEndpoint) { url = model.voiceEndpoint }
        .onChange(of: model.voiceEndpointModel) { name = model.voiceEndpointModel }
    }

    private var testButton: some View {
        Button(model.voiceTesting ? "Testing…" : "Test") { Task { await model.testVoiceModel() } }
            .buttonStyle(.brand)
            .disabled(model.voiceTesting)
    }

    private func field(_ label: String, _ hint: String, _ text: Binding<String>) -> some View {
        HStack {
            Text(label).frame(width: 90, alignment: .leading)
            TextField("", text: text)
                .textFieldStyle(.plain)
                .brandPlaceholder(hint, shown: text.wrappedValue.isEmpty)
                .padding(.vertical, 6)
                .padding(.horizontal, 10)
                .background(Brand.row, in: Rectangle())
                .accessibilityLabel(label)
        }
    }
}
