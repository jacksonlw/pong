import SwiftUI
import XCTest
@testable import PongKit

@MainActor
final class JoinRequestTests: XCTestCase {
    private func set(_ waiting: [DeviceSet.Asking]) -> DeviceSet {
        DeviceSet(mode: "local", why: nil, account: nil, devices: [], waiting: waiting)
    }

    private let phone = DeviceSet.Asking(id: "dd44", name: "iPhone", platform: "ios", code: "960644")
    private let box = DeviceSet.Asking(id: "cc33", name: "Box", platform: "linux", code: "042917")

    func testADeviceAskingToJoinIsShown() async throws {
        let m = JoinRequests(api: try FakeDevices())
        await m.load()
        XCTAssertEqual(m.shown?.code, "042917")
    }

    func testApprovingAdmitsTheDeviceAndNothingIsShown() async throws {
        let api = try FakeDevices()
        let m = JoinRequests(api: api)
        await m.load()
        await m.approve()
        let approved = await api.approved
        XCTAssertEqual(approved, ["cc33"])
        XCTAssertNil(m.shown)
    }

    func testClosingHidesThatDeviceUntilItStopsAsking() throws {
        let m = JoinRequests(api: try FakeDevices())
        m.update(set([box]))
        m.close()
        XCTAssertNil(m.shown)
        m.update(set([box]))
        XCTAssertNil(m.shown, "still asking, still closed")
        m.update(set([]))
        m.update(set([box]))
        XCTAssertEqual(m.shown?.id, "cc33", "it asked again")
    }

    func testClosingOneShowsTheNextAsking() throws {
        let m = JoinRequests(api: try FakeDevices())
        m.update(set([box, phone]))
        XCTAssertEqual(m.shown?.id, "cc33")
        m.close()
        XCTAssertEqual(m.shown?.id, "dd44")
    }

    func testThePanelShowsTheAskingDeviceAndItsCode() throws {
        let m = JoinRequests(api: try FakeDevices())
        m.update(set([phone]))
        try assertSnapshot(JoinRequestView(model: m).frame(width: JoinRequestView.width), named: "join-request")
    }
}
