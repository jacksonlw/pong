import AppKit
import SwiftUI
import XCTest
@testable import PongKit

/// Stands in for the Engine with responses recorded from a real one (Fixtures/start/).
actor FakeStarter: StartAPI {
    static let dir = CardSnapshotTests.here.appendingPathComponent("Fixtures/start")

    var projectList: [StartProject]
    var harnessList: [StartHarness]
    /// The Engine's recorded 422 for Codex; other Harnesses start.
    let refusal: String
    var started: [String] = []
    var calls: [String] = []
    var failing: String?
    let current: EngineSettings

    init(empty: Bool = false) throws {
        func load<T: Decodable>(_ name: String) throws -> T {
            try JSON.decoder.decode(T.self, from: Data(contentsOf: Self.dir.appendingPathComponent(name)))
        }
        projectList = empty ? [] : try load("projects.json")
        harnessList = try load("harnesses.json")
        struct Body: Decodable { var error: String }
        refusal = try (load("start-refused.json") as Body).error
        current = try JSON.decoder.decode(EngineSettings.self, from: Data(contentsOf: FakeEngine.dir.appendingPathComponent("settings.json")))
    }

    func fail(with message: String?) { failing = message }
    private func check() throws {
        if let failing { throw EngineClient.Failure(description: failing) }
    }
    func settings() async throws -> EngineSettings { current }
    private func change(_ id: String, _ f: (inout Project) -> Void) -> Project {
        let i = projectList.firstIndex { $0.project.id == id }!
        f(&projectList[i].project)
        return projectList[i].project
    }
    func changeProject(_ id: String, name: String?, colour: String?) async throws -> Project {
        try check()
        calls.append("change \(id) \(name ?? "-") \(colour ?? "-")")
        return change(id) { p in
            if let name { p.name = name }
            if let colour { p.colour = colour }
        }
    }
    func setLogo(_ id: String, image: Data) async throws -> Project {
        try check()
        let rep = NSBitmapImageRep(data: image)
        calls.append("logo \(id) \(image.starts(with: [0x89, 0x50, 0x4E, 0x47]) ? "png" : "other") \(rep?.pixelsWide ?? 0)x\(rep?.pixelsHigh ?? 0)")
        return change(id) { $0.logo = "/projects/\(id)/logo?v=2" }
    }
    func removeLogo(_ id: String) async throws -> Project {
        try check()
        calls.append("remove logo \(id)")
        return change(id) { $0.logo = nil }
    }

    func startProjects() async throws -> [StartProject] { projectList }
    var prompts: [String: [String]] = [:]
    func setPrompts(_ p: [String], for project: String) { prompts[project] = p }
    func recentPrompts(project: String) async throws -> [String] { prompts[project] ?? [] }
    func logo(_ path: String) async throws -> Data { try Data(contentsOf: Self.dir.appendingPathComponent("shop-logo.png")) }
    func startHarnesses() async throws -> [StartHarness] { harnessList }
    func cannotStart(_ id: String) {
        for i in harnessList.indices where harnessList[i].status.id == id { harnessList[i].canStart = false }
    }
    /// Runs while a Session starts, as the human might type then.
    var whileStarting: (@Sendable () async -> Void)?
    func onStart(_ f: @escaping @Sendable () async -> Void) { whileStarting = f }

    func startSession(harness: String, at place: StartPlace, prompt: String) async throws {
        started.append("\(harness) \(place) \(prompt)")
        await whileStarting?()
        if harness == "codex" { throw StartRefused(message: refusal) }
    }
}

@MainActor
final class StartPanelTests: XCTestCase {
    func loaded(empty: Bool = false) async throws -> (StartPanelModel, FakeStarter) {
        let engine = try FakeStarter(empty: empty)
        let model = StartPanelModel(api: engine)
        await model.load()
        return (model, engine)
    }

    func id(_ model: StartPanelModel, _ name: String) -> String { model.projects.first { $0.name == name }!.id }

    func testListsProjectsWithLogosAndOffersHarnessesThatCanStart() async throws {
        let (model, engine) = try await loaded()
        XCTAssertEqual(model.projects.map(\.name), ["shop", "billing-api"])
        XCTAssertNotNil(model.logos[id(model, "shop")])
        XCTAssertEqual(model.harnesses.map(\.id), ["claude-code", "codex"], "only installed ones the Engine can start")
        await engine.cannotStart("codex")
        await model.load()
        XCTAssertEqual(model.harnesses.map(\.id), ["claude-code"])
        model.choose(project: id(model, "shop"))
        XCTAssertEqual(model.harness, "claude-code", "codex, last used in shop, is not offered")
    }

    func testHarnessDefaultsToTheOneLastUsedInTheProject() async throws {
        let (model, _) = try await loaded()
        model.choose(project: id(model, "shop"))
        XCTAssertEqual(model.harness, "codex")
        model.choose(project: id(model, "billing-api"))
        XCTAssertEqual(model.harness, "claude-code")
    }

    func testStartingNeedsAPlaceAndText() async throws {
        let (model, engine) = try await loaded()
        model.text = "Add invoices"
        XCTAssertFalse(model.canStart, "no Project yet")
        let refused = await model.start()
        XCTAssertFalse(refused)
        model.choose(project: id(model, "billing-api"))
        model.text = "   "
        XCTAssertFalse(model.canStart, "no text")
        model.text = "Add invoices\n"
        XCTAssertTrue(model.canStart)
        let ok = await model.start()
        XCTAssertTrue(ok)
        let calls = await engine.started
        XCTAssertEqual(calls, ["claude-code project(\"\(id(model, "billing-api"))\") Add invoices"])
        XCTAssertEqual(model.text, "", "ready for the next one")
        XCTAssertNil(model.problem)
    }

    func testRefusalKeepsTheTextAndShowsTheEnginesWords() async throws {
        let (model, _) = try await loaded()
        model.choose(project: id(model, "shop"))
        model.text = "Fix the cart total"
        let ok = await model.start()
        XCTAssertFalse(ok)
        XCTAssertEqual(model.text, "Fix the cart total")
        XCTAssertEqual(model.problem, #"Pong cannot start Sessions in the Harness "codex""#)
    }

    func testTextTypedWhileStartingStaysInTheDraft() async throws {
        let (model, engine) = try await loaded()
        model.choose(project: id(model, "billing-api"))
        model.text = "Add invoices"
        await engine.onStart { await MainActor.run { model.text = "Then the refunds" } }
        let ok = await model.start()
        XCTAssertTrue(ok)
        XCTAssertEqual(model.text, "Then the refunds", "not what went, so it is kept")
    }

    func testAnotherFolder() async throws {
        let (model, engine) = try await loaded(empty: true)
        model.choose(folder: "/tmp/new-thing")
        XCTAssertEqual(model.folderName, "new-thing")
        model.text = "Sketch it"
        await model.start()
        let calls = await engine.started
        XCTAssertEqual(calls, [#"claude-code folder("/tmp/new-thing") Sketch it"#])
    }

    // The draft, its bullets and its files.

    func testTheDraftOutlivesThePanelAndTheAppUntilASessionStarts() async throws {
        let name = "pong-start-draft-\(UUID().uuidString)"
        let kept = UserDefaults(suiteName: name)!
        defer { kept.removePersistentDomain(forName: name) }
        let engine = try FakeStarter()
        var model = StartPanelModel(api: engine, drafts: kept)
        model.text = "Add invoices\n• to the export"
        let folder = FileManager.default.temporaryDirectory.appendingPathComponent("start-files-\(UUID())")
        try FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: folder) }
        let spec = folder.appendingPathComponent("spec.md").path, shot = folder.appendingPathComponent("shot.png").path
        try Data("spec".utf8).write(to: URL(fileURLWithPath: spec))
        model.add(files: [spec, spec, shot])
        model.remove(file: shot)

        model = StartPanelModel(api: engine, drafts: kept) // the panel opened again, or Pong did
        XCTAssertEqual(model.text, "Add invoices\n• to the export")
        XCTAssertEqual(model.files, [spec])

        await model.load()
        model.choose(project: id(model, "billing-api"))
        let ok = await model.start()
        XCTAssertTrue(ok)
        let calls = await engine.started
        XCTAssertEqual(calls.last?.hasSuffix("Add invoices\n- to the export\n\nFiles:\n- \(spec)"), true, "bullets go as Markdown, files by path")
        model = StartPanelModel(api: engine, drafts: kept)
        XCTAssertEqual(model.text, "")
        XCTAssertEqual(model.files, [])
    }

    /// The panel opens as it was closed, on the same Project or folder. A Session that starts
    /// clears the place with the draft; a Project gone since is dropped.
    func testThePanelOpensAsItWasLeft() async throws {
        let name = "pong-start-state-\(UUID().uuidString)"
        let kept = UserDefaults(suiteName: name)!
        defer { kept.removePersistentDomain(forName: name) }
        let engine = try FakeStarter()
        var model = StartPanelModel(api: engine, drafts: kept)
        await model.load()
        let billing = id(model, "billing-api")
        model.choose(project: billing)

        model = StartPanelModel(api: engine, drafts: kept)
        XCTAssertEqual(model.place, .project(billing))
        await model.load()
        XCTAssertEqual(model.place, .project(billing))
        XCTAssertEqual(model.harness, "claude-code", "the Harness last used in it")

        model.choose(folder: "/tmp/new-thing")
        model = StartPanelModel(api: engine, drafts: kept)
        XCTAssertEqual(model.place, .folder("/tmp/new-thing"))

        model.text = "Sketch it"
        await model.load()
        await model.start()
        XCTAssertNil(StartPanelModel(api: engine, drafts: kept).place)

        kept.set("gone", forKey: "startPlaceProject")
        model = StartPanelModel(api: engine, drafts: kept)
        await model.load()
        XCTAssertNil(model.place)
    }

    func testSnapshotFilesAndList() async throws {
        let (model, _) = try await loaded()
        model.choose(project: id(model, "billing-api"))
        model.text = "Add invoices to the export:\n• one row per invoice\n• totals in the last row"
        model.add(files: ["/Users/me/shop/docs/export-format.md", "/Users/me/Desktop/Screenshot 2026-11-19 at 10.12.31.png"])
        try assertPanel(model, named: "start-files")
    }

    // The Project menu.

    func testProjectMenuChangesGoToTheEngineAndShow() async throws {
        let (model, engine) = try await loaded()
        XCTAssertEqual(model.palette.count, 8)
        let shop = id(model, "shop"), billing = id(model, "billing-api")
        await model.rename(billing, to: "Billing")
        await model.rename(billing, to: "Billing") // unchanged: nothing sent
        await model.setColour(billing, "#0090FF")
        // A large SVG is chosen: the Engine gets a small PNG of it, in its proportions.
        let svg = Data(#"<svg xmlns="http://www.w3.org/2000/svg" width="4000" height="2000"><rect width="4000" height="2000"/></svg>"#.utf8)
        await model.setLogo(billing, image: svg)
        await model.removeLogo(shop)
        let calls = await engine.calls
        XCTAssertEqual(calls, ["change \(billing) Billing -", "change \(billing) - #0090FF",
                               "logo \(billing) png 256x128", "remove logo \(shop)"])
        XCTAssertNil(model.problem)
        XCTAssertEqual(model.projects[1].name, "Billing")
        XCTAssertEqual(model.projects[1].colour, "#0090FF")
        XCTAssertNotNil(model.logos[billing])
        XCTAssertNil(model.logos[shop])
        XCTAssertNil(model.projects[0].logo)
    }

    func testProjectMenuShowsTheEnginesRefusal() async throws {
        let (model, engine) = try await loaded()
        await engine.fail(with: "the logo could not be kept: the disk is full")
        await model.setLogo(id(model, "shop"), image: try Data(contentsOf: FakeStarter.dir.appendingPathComponent("shop-logo.png")))
        XCTAssertTrue(model.problem?.contains("the disk is full") == true)
        XCTAssertNotNil(model.logos[id(model, "shop")], "the old logo stays")
    }

    func testAFileThatIsNoImageIsNotSentAsALogo() async throws {
        let (model, engine) = try await loaded()
        await model.setLogo(id(model, "shop"), image: Data("not an image".utf8))
        let calls = await engine.calls
        XCTAssertEqual(calls, [])
        XCTAssertEqual(model.problem, "Pong could not read that image. Choose another file.")
    }

    func testProjectMenuIsBuiltFromItsSections() async throws {
        let (model, _) = try await loaded()
        XCTAssertEqual(ProjectMenuSection.standard(model: model, chooseLogo: { _ in }).map(\.title), ["Name", "Colour", "Logo", "Other Devices"])
    }

    func testSnapshotGearAndProjectMenu() async throws {
        let (model, _) = try await loaded()
        model.menuOpen = id(model, "shop") // the gear shows while its menu is open, as on hover and focus
        try assertPanel(model, named: "start-gear")
        for p in model.projects {
            let menu = ProjectMenu(project: p, logo: model.logos[p.id],
                                   sections: ProjectMenuSection.standard(model: model, chooseLogo: { _ in }), problem: nil)
            try CardSnapshotTests().assertSnapshot(menu, named: "project-menu-\(p.name)")
        }
    }

    // Snapshots from the recorded responses.

    func testSnapshotProjects() async throws {
        let (model, _) = try await loaded()
        model.choose(project: id(model, "billing-api"))
        model.text = "Add invoices to the export"
        try assertPanel(model, named: "start-projects")
    }

    func testSnapshotNoProjects() async throws {
        let (model, _) = try await loaded(empty: true)
        try assertPanel(model, named: "start-no-projects")
    }

    func testSnapshotRefused() async throws {
        let (model, _) = try await loaded()
        model.choose(project: id(model, "shop"))
        model.text = "Fix the cart total"
        await model.start()
        try assertPanel(model, named: "start-refused")
    }

    func assertPanel(_ model: StartPanelModel, named name: String, file: StaticString = #filePath, line: UInt = #line) throws {
        let snap = CardSnapshotTests()
        for (theme, appearance) in [("light", NSAppearance.Name.aqua), ("dark", .darkAqua)] {
            // As the Bar shows it, open into the start panel.
            let bar = BarView(figures: BarFigures(), threads: [], open: .start, start: StartPanelView(model: model))
            let png = snap.render(bar.environment(\.typingAnimates, false), appearance: appearance)
            let ref = CardSnapshotTests.here.appendingPathComponent("Snapshots/\(name)-\(theme).png")
            if ProcessInfo.processInfo.environment["PONG_RECORD_SNAPSHOTS"] != nil || !FileManager.default.fileExists(atPath: ref.path) {
                try png.write(to: ref)
                XCTFail("recorded \(ref.lastPathComponent); look at it, then run again", file: file, line: line)
                continue
            }
            let diff = try CardSnapshotTests.difference(png, Data(contentsOf: ref))
            if diff > 0.002 {
                let failed = FileManager.default.temporaryDirectory.appendingPathComponent("\(name)-\(theme)-actual.png")
                try png.write(to: failed)
                XCTFail("\(name)-\(theme): \(String(format: "%.2f", diff * 100))% of pixels differ; actual in \(failed.path)", file: file, line: line)
            }
        }
    }

    // MARK: Recent prompts

    func testUpStepsBackThroughTheProjectsRecentPromptsAndDownReturnsToWhatWasTyped() async throws {
        let (model, engine) = try await loaded()
        let shop = id(model, "shop")
        await engine.setPrompts(["Fix the cart total", "Add a coupon field"], for: shop)
        model.choose(project: shop)
        await model.loadRecent(project: shop)
        model.text = "half typed"
        XCTAssertTrue(model.recallOlder())
        XCTAssertEqual(model.text, "Fix the cart total")
        XCTAssertTrue(model.recallOlder())
        XCTAssertEqual(model.text, "Add a coupon field")
        XCTAssertTrue(model.recallOlder(), "the oldest stays")
        XCTAssertEqual(model.text, "Add a coupon field")
        XCTAssertTrue(model.recallNewer())
        XCTAssertEqual(model.text, "Fix the cart total")
        XCTAssertTrue(model.recallNewer())
        XCTAssertEqual(model.text, "half typed", "past the newest is what was typed")
        XCTAssertFalse(model.recallNewer(), "↓ moves the caret again")
    }

    func testARecalledPromptIsTheDraft() async throws {
        let engine = try FakeStarter()
        let defaults = UserDefaults(suiteName: "recall-\(UUID())")!
        let model = StartPanelModel(api: engine, drafts: defaults)
        await model.load()
        let shop = id(model, "shop")
        await engine.setPrompts(["Fix the cart total"], for: shop)
        model.choose(project: shop)
        await model.loadRecent(project: shop)
        model.recallOlder()
        let reopened = StartPanelModel(api: engine, drafts: defaults)
        XCTAssertEqual(reopened.text, "Fix the cart total")
    }

    func testNoRecentPromptsLeavesUpToTheCaret() async throws {
        let (model, _) = try await loaded()
        model.choose(project: id(model, "billing-api"))
        XCTAssertFalse(model.recallOlder())
        model.choose(folder: "/tmp")
        XCTAssertFalse(model.recallOlder(), "a folder has no Project's prompts")
    }

    func testChangingProjectForgetsWhereUpWas() async throws {
        let (model, engine) = try await loaded()
        let shop = id(model, "shop"), billing = id(model, "billing-api")
        await engine.setPrompts(["Fix the cart total"], for: shop)
        await engine.setPrompts(["Add invoices"], for: billing)
        model.choose(project: shop)
        await model.loadRecent(project: shop)
        model.recallOlder()
        model.choose(project: billing)
        await model.loadRecent(project: billing)
        XCTAssertNil(model.recalled)
        model.recallOlder()
        XCTAssertEqual(model.text, "Add invoices")
    }

    func testAFileThatCannotBeReadKeepsTheDraftAndSaysWhich() async throws {
        let (model, engine) = try await loaded()
        model.choose(project: id(model, "billing-api"))
        model.text = "Add invoices"
        model.add(files: ["/nowhere/spec.md"])
        let ok = await model.start()
        XCTAssertFalse(ok)
        XCTAssertEqual(model.problem, "The File spec.md cannot be read, so nothing was sent. Remove it or add it again.")
        XCTAssertEqual(model.text, "Add invoices")
        let calls = await engine.started
        XCTAssertEqual(calls, [])
    }
}
