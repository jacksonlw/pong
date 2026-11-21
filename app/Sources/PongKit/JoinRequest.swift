import SwiftUI

/// Which Device asking to join the set the panel beside the Bar shows: the first one asking
/// that the human has not closed. Closing it hides that Device until it stops asking.
@MainActor @Observable
public final class JoinRequests {
    public private(set) var shown: DeviceSet.Asking?
    public private(set) var problem: String?
    @ObservationIgnored private var waiting: [DeviceSet.Asking] = []
    @ObservationIgnored private var closed: Set<String> = []
    @ObservationIgnored private let api: DevicesAPI

    public init(api: DevicesAPI) { self.api = api }

    public func update(_ set: DeviceSet) {
        waiting = set.waiting
        closed.formIntersection(set.waiting.map(\.id))
        let next = waiting.first { !closed.contains($0.id) }
        if next?.id != shown?.id { problem = nil }
        shown = next
    }

    public func load() async {
        if let set = try? await api.devices() { update(set) }
    }

    /// Reloads every two seconds until the task is cancelled.
    public func follow() async {
        while !Task.isCancelled {
            await load()
            try? await Task.sleep(for: .seconds(2))
        }
    }

    public func close() {
        guard let s = shown else { return }
        closed.insert(s.id)
        shown = waiting.first { !closed.contains($0.id) }
        problem = nil
    }

    public func approve() async {
        guard let s = shown else { return }
        do { update(try await api.approveDevice(s.id)) } catch { problem = "\(error)" }
    }
}

/// One Device asking to join: its name, its code and Approve. Settings and the panel beside the
/// Bar both show it.
struct AskingRow: View {
    var asking: DeviceSet.Asking
    var approve: () -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            Text("\(asking.name) (\(platformTitle(asking.platform))) asks to join")
            HStack {
                Text("Code \(asking.code)").brandStyle(.question)
                Spacer()
                Button("Approve", action: approve).buttonStyle(.brandPrimary)
            }
            Text("Approve only if the same code shows on \(asking.name).")
                .brandStyle(.secondary)
                .foregroundStyle(Brand.secondary)
        }
    }
}

/// The panel beside the Bar while a Device asks to join.
public struct JoinRequestView: View {
    public static let width: CGFloat = 360
    public static let height: CGFloat = 180
    var model: JoinRequests

    public init(model: JoinRequests) { self.model = model }

    public var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            if let a = model.shown {
                AskingRow(asking: a) { Task { await model.approve() } }
            }
            if let p = model.problem {
                Text(p).brandStyle(.secondary).foregroundStyle(Brand.warning)
            }
        }
        .padding(.horizontal, 16)
        .padding(.bottom, 16)
        .foregroundStyle(Brand.text)
        .background(Brand.page)
    }
}
