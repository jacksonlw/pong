import AppKit
import PongKit
import SwiftUI

/// The panel beside the Bar while a Device asks to join the set, in the same row as Settings.
/// It opens when a Device starts asking and closes once none is shown.
@MainActor
final class JoinWindowController {
    private let client: @Sendable () async -> EngineClient?
    private let dock: Dock
    private var panel: DockPanel?
    private var model: JoinRequests?
    private var following: Task<Void, Never>?
    private static let key = "join"

    init(client: @escaping @Sendable () async -> EngineClient?, dock: Dock) {
        self.client = client
        self.dock = dock
    }

    func start() {
        following = Task { [weak self] in
            guard let self else { return }
            while !Task.isCancelled, model == nil {
                if let c = await client() { model = JoinRequests(api: c) } else { try? await Task.sleep(for: .seconds(1)) }
            }
            guard let model else { return }
            track()
            await model.follow()
        }
    }

    func stop() { following?.cancel() }

    private func track() {
        guard let model else { return }
        withObservationTracking { _ = model.shown } onChange: { [weak self] in
            DispatchQueue.main.async { self?.changed(); self?.track() }
        }
    }

    private func changed() {
        guard let model else { return }
        if model.shown == nil { remove() } else if panel == nil { open(model) }
    }

    private func open(_ model: JoinRequests) {
        let dock = dock
        let view = VStack(spacing: 0) {
            PanelTitle(symbol: "person.badge.plus", title: "A Device asks to join", closeLabel: "Close",
                       moved: { dock.dropped(Self.key) }, putBack: { dock.putBack(Self.key) },
                       onClose: { model.close() })
            JoinRequestView(model: model)
            Spacer(minLength: 0)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(Brand.page)
        let p = DockPanel.make(size: CGSize(width: JoinRequestView.width, height: JoinRequestView.height))
        let host = Zoom.shared.host(view)
        host.sizingOptions = [] // the human sizes the panel by its edges
        p.host(host, minimum: CGSize(width: JoinRequestView.width, height: JoinRequestView.height))
        panel = p
        dock.show(Self.key, p, closed: { model.close() })
        print("join request shown")
    }

    private func remove() {
        guard panel != nil else { return }
        panel = nil
        dock.remove(Self.key)
    }
}
