import SwiftUI
import XCTest
@testable import PongKit

actor FakeDevices: DevicesAPI {
    var current: DeviceSet
    var approved: [String] = []

    init() throws {
        current = try JSON.decoder.decode(DeviceSet.self, from: Data(contentsOf:
            CardSnapshotTests.here.appendingPathComponent("Fixtures/devices/waiting.json")))
    }

    func devices() async throws -> DeviceSet { current }
    func computers() async throws -> [ComputerState] {
        [ComputerState(id: "aa11", name: "Laptop", reachable: true,
                       harnesses: [HarnessStatus(id: "claude-code", name: "Claude Code", installed: true, connected: true, version: "2.1.3")],
                       projects: ["git:github.com/me/shop"], running: 2)]
    }
    func revokeDevice(_ id: String) async throws -> DeviceSet {
        current.devices.removeAll { $0.id == id }
        return current
    }

    var settled: [String] = []
    func setFork(_ f: DeviceSet.Fork) { current.fork = f }
    func settleFork(_ keep: String) async throws -> DeviceSet {
        settled.append(keep)
        current.fork = nil
        return current
    }

    func approveDevice(_ id: String) async throws -> DeviceSet {
        approved.append(id)
        let a = current.waiting.first { $0.id == id }!
        current.waiting.removeAll { $0.id == id }
        current.devices.append(.init(id: id, name: a.name, platform: a.platform, engine: true, this: false, reachable: true))
        return current
    }
}

@MainActor
final class DevicesTests: XCTestCase {
    func testApprovingAWaitingDeviceAdmitsIt() async throws {
        let api = try FakeDevices()
        let m = DevicesModel(api: api)
        await m.load()
        XCTAssertEqual(m.set?.waiting.first?.code, "042917")
        await m.approve("cc33")
        let approved = await api.approved
        XCTAssertEqual(approved, ["cc33"])
        XCTAssertEqual(m.set?.waiting, [])
        XCTAssertEqual(m.set?.devices.map(\.name), ["Laptop", "Desktop", "Box"])
    }

    func testRevokingAsksFirstThenTakesTheDeviceOut() async throws {
        let m = DevicesModel(api: try FakeDevices())
        await m.load()
        m.confirmingRevoke = "bb22"
        await m.revoke("bb22")
        XCTAssertNil(m.confirmingRevoke)
        XCTAssertEqual(m.set?.devices.map(\.name), ["Laptop"])
    }

    /// Two Devices that removed each other wait for the human, who says which is theirs.
    func testAForkWaitsForTheHumanWhoKeepsOneSide() async throws {
        let api = try FakeDevices()
        await api.setFork(.init(ours: [.init(id: "aa11", name: "Laptop", platform: "mac", engine: true, this: true, reachable: true)],
                                theirs: [.init(id: "bb22", name: "Desktop", platform: "mac", engine: true, this: false, reachable: false)],
                                mutual: true))
        let m = DevicesModel(api: api)
        await m.load()
        try assertSnapshot(VStack(alignment: .leading, spacing: 12) { DevicesSection(model: m) }
            .brandStyle(.body).foregroundStyle(Brand.text).padding(14).frame(width: 512).background(Brand.surface),
            named: "devices-fork")
        await m.settleFork(keepOurs: true)
        let settled = await api.settled
        XCTAssertEqual(settled, ["ours"])
        XCTAssertNil(m.set?.fork)
    }

    func testTheSectionShowsTheAskingDeviceAndItsCode() async throws {
        let m = DevicesModel(api: try FakeDevices())
        await m.load()
        try assertSnapshot(VStack(alignment: .leading, spacing: 12) { DevicesSection(model: m) }
            .brandStyle(.body).foregroundStyle(Brand.text).padding(14).frame(width: 512).background(Brand.surface),
            named: "devices-waiting")
    }
}

@MainActor
final class ComputerChoiceTests: XCTestCase {
    func testTheStartPanelShowsTheComputerPongChoosesAndWhyOthersCannot() throws {
        let some = Placement(choice: "d", computers: [
            .init(id: "d", name: "Desktop", eligible: true, reasons: nil, warning: nil, running: 0),
            .init(id: "b", name: "Box", eligible: true, reasons: nil, warning: "Claude Code 3.0 is outside the tested range", running: 2),
            .init(id: "l", name: "Laptop", eligible: false, reasons: ["the Project is not on it"], warning: nil, running: 0),
        ])
        let none = Placement(choice: nil, computers: [
            .init(id: "d", name: "Desktop", eligible: false, reasons: ["it cannot be reached"], warning: nil, running: 0),
            .init(id: "b", name: "Box", eligible: false, reasons: ["the Project is not enabled on it"], warning: nil, running: 0),
        ])
        let view = VStack(alignment: .leading, spacing: 16) {
            ComputerChoice(placement: some, chosen: "b") { _ in }
            ComputerChoice(placement: none, chosen: nil) { _ in }
        }
        .brandStyle(.body).foregroundStyle(Brand.text).padding(14).frame(width: 420, alignment: .leading).background(Brand.surface)
        try assertSnapshot(view, named: "computer-choice")
    }
}
