import SwiftUI

/// The set of Devices as this Mac's Engine reports it (`GET /devices`).
public struct DeviceSet: Decodable, Sendable, Equatable {
    public struct Device: Decodable, Sendable, Equatable, Identifiable {
        public var id: String
        public var name: String
        public var platform: String
        public var engine: Bool
        public var this: Bool
        public var reachable: Bool
    }
    public struct Asking: Decodable, Sendable, Equatable, Identifiable {
        public var id: String
        public var name: String
        public var platform: String
        public var code: String
    }
    public var mode: String
    /// Why the set is on Local while this Device is registered with an Account.
    public var why: String?
    public var account: Bool?
    public var devices: [Device]
    public var waiting: [Asking]
}

/// A computer's state as it reports it (`GET /computers`).
public struct ComputerState: Decodable, Sendable, Equatable, Identifiable {
    public var id: String
    public var name: String
    public var reachable: Bool
    public var harnesses: [HarnessStatus]
    public var projects: [String]
    public var running: Int

    /// One line for Settings: its Sessions, its Harnesses and the Projects it takes from elsewhere.
    public var summary: String {
        var parts = ["\(running) Session\(running == 1 ? "" : "s") running"]
        for h in harnesses where h.installed {
            let v = h.version.map { " \($0)" } ?? ""
            parts.append("\(h.name)\(v) \(h.connected ? "connected" : "not connected")")
        }
        parts.append("takes Sessions in \(projects.count) Project\(projects.count == 1 ? "" : "s")")
        return parts.joined(separator: " · ")
    }
}

public protocol DevicesAPI: Sendable {
    func computers() async throws -> [ComputerState]
    func devices() async throws -> DeviceSet
    func approveDevice(_ id: String) async throws -> DeviceSet
    func revokeDevice(_ id: String) async throws -> DeviceSet
    func cloudSignIn(address: String, code: String?) async throws -> DeviceSet
}

public extension DevicesAPI {
    func computers() async throws -> [ComputerState] { [] }
    func cloudSignIn(address: String, code: String?) async throws -> DeviceSet { try await devices() }
}

extension EngineClient: DevicesAPI {
    public func computers() async throws -> [ComputerState] { try await json("/computers") }
    public func cloudSignIn(address: String, code: String?) async throws -> DeviceSet {
        try await json("/devices/cloud/sign-in", "POST", JSONEncoder().encode(["address": address, "code": code ?? ""]))
    }
    public func devices() async throws -> DeviceSet { try await json("/devices") }
    public func approveDevice(_ id: String) async throws -> DeviceSet { try await json("/devices/\(id)/approve", "POST") }
    public func revokeDevice(_ id: String) async throws -> DeviceSet { try await json("/devices/\(id)/revoke", "POST") }
}

/// What Settings shows about the set, refreshed while Settings is open.
@MainActor @Observable
public final class DevicesModel {
    public private(set) var set: DeviceSet?
    public private(set) var computers: [String: ComputerState] = [:]
    public private(set) var problem: String?
    @ObservationIgnored private let api: DevicesAPI

    public init(api: DevicesAPI) { self.api = api }

    public func load() async {
        do { set = try await api.devices(); problem = nil } catch { problem = "\(error)" }
        if let cs = try? await api.computers() {
            computers = Dictionary(cs.map { ($0.id, $0) }, uniquingKeysWith: { a, _ in a })
        }
    }

    /// Reloads every two seconds until the task is cancelled, so a Device asking to join shows up.
    public func follow() async {
        while !Task.isCancelled {
            await load()
            try? await Task.sleep(for: .seconds(2))
        }
    }

    /// The Device the human asked to revoke, waiting for them to confirm.
    public var confirmingRevoke: String?

    /// Signing in to Pong Cloud: the address typed, and whether a code was sent to it.
    public var address = ""
    public var code = ""
    public private(set) var codeSent = false

    public func sendCode() async {
        do { _ = try await api.cloudSignIn(address: address, code: nil); codeSent = true; problem = nil } catch { problem = "\(error)" }
    }

    public func signIn() async {
        do {
            set = try await api.cloudSignIn(address: address, code: code)
            codeSent = false
            code = ""
            problem = nil
        } catch { problem = "\(error)" }
    }

    public func revoke(_ id: String) async {
        confirmingRevoke = nil
        do { set = try await api.revokeDevice(id); problem = nil } catch { problem = "\(error)" }
    }

    public func approve(_ id: String) async {
        do { set = try await api.approveDevice(id); problem = nil } catch { problem = "\(error)" }
    }
}

func platformTitle(_ p: String) -> String {
    switch p {
    case "mac": "Mac"
    case "linux": "Linux"
    case "ios": "iPhone"
    default: p
    }
}

/// Settings' Devices section: the way the set is connected, each Device and whether it is
/// reachable, and any Device asking to join, with its code and Approve.
struct DevicesSection: View {
    @Bindable var model: DevicesModel

    private var connection: String {
        guard let set = model.set else { return "" }
        if set.mode == "cloud" {
            return "Connected through Pong Cloud: your Devices reach each other from any network, sealed so Pong's server cannot read them."
        }
        if set.account == true, let why = set.why, !why.isEmpty {
            return "Connected on Local, because \(why): your Devices talk directly over this network."
        }
        return "Connected on Local: your Devices talk directly over this network, and nothing passes through Pong's servers."
    }

    var body: some View {
        Text(connection)
            .brandStyle(.secondary)
            .foregroundStyle(Brand.secondary)
            .fixedSize(horizontal: false, vertical: true)
        if model.set != nil, model.set?.account != true {
            VStack(alignment: .leading, spacing: 6) {
                Text("Sign in to Pong Cloud to reach your Devices from any network. Your other Devices follow without signing in.")
                    .brandStyle(.secondary)
                    .foregroundStyle(Brand.secondary)
                    .fixedSize(horizontal: false, vertical: true)
                HStack {
                    TextField("", text: $model.address)
                        .brandPlaceholder("Email or phone", shown: model.address.isEmpty)
                        .textFieldStyle(.plain)
                        .padding(8)
                        .background(Brand.row, in: Rectangle())
                    if model.codeSent {
                        TextField("", text: $model.code)
                            .brandPlaceholder("Code", shown: model.code.isEmpty)
                            .textFieldStyle(.plain)
                            .frame(width: 80)
                            .padding(8)
                            .background(Brand.row, in: Rectangle())
                        Button("Sign in") { Task { await model.signIn() } }.buttonStyle(.brandPrimary)
                    } else {
                        Button("Send code") { Task { await model.sendCode() } }.buttonStyle(.brand)
                    }
                }
            }
        }
        if let set = model.set {
            ForEach(set.devices) { d in
                HStack {
                    Text(d.name)
                    Text(platformTitle(d.platform)).foregroundStyle(Brand.secondary)
                    Spacer()
                    Text(d.this ? "This Mac" : d.reachable ? "Reachable" : "Unreachable")
                        .foregroundStyle(d.this || d.reachable ? Brand.text : Brand.secondary)
                    if !d.this {
                        Button("Revoke") { model.confirmingRevoke = d.id }
                            .buttonStyle(.brand)
                    }
                }
                if let c = model.computers[d.id], c.reachable, d.engine {
                    Text(c.summary)
                        .brandStyle(.secondary)
                        .foregroundStyle(Brand.secondary)
                        .fixedSize(horizontal: false, vertical: true)
                    ForEach(c.harnesses.filter { $0.warning != nil }, id: \.id) { h in
                        Text(h.warning ?? "").brandStyle(.secondary).foregroundStyle(Brand.warning)
                    }
                }
                if model.confirmingRevoke == d.id {
                    HStack {
                        Text("\(d.name) will stop being able to answer or start anything, at once.")
                            .brandStyle(.secondary)
                            .foregroundStyle(Brand.secondary)
                            .fixedSize(horizontal: false, vertical: true)
                        Spacer()
                        Button("Cancel") { model.confirmingRevoke = nil }.buttonStyle(.brand)
                        Button("Revoke \(d.name)") { Task { await model.revoke(d.id) } }.buttonStyle(.brandPrimary)
                    }
                }
            }
            ForEach(set.waiting) { a in
                VStack(alignment: .leading, spacing: 6) {
                    Text("\(a.name) (\(platformTitle(a.platform))) asks to join")
                    HStack {
                        Text("Code \(a.code)").brandStyle(.question)
                        Spacer()
                        Button("Approve") { Task { await model.approve(a.id) } }
                            .buttonStyle(.brandPrimary)
                    }
                    Text("Approve only if the same code shows on \(a.name).")
                        .brandStyle(.secondary)
                        .foregroundStyle(Brand.secondary)
                }
            }
            if set.devices.count == 1 && set.waiting.isEmpty {
                Text("To add a computer, run pong devices pair on it.")
                    .brandStyle(.secondary)
                    .foregroundStyle(Brand.secondary)
            }
        }
        if let p = model.problem {
            Text(p).brandStyle(.secondary).foregroundStyle(Brand.warning)
        }
    }
}
