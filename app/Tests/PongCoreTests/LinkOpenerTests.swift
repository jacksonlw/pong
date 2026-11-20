import Foundation
import Testing
import PongCore

/// What opening a link Resource does, through a fake of the system, and the rules of the row of Tags.
@MainActor
struct LinkOpenerTests {
    final class FakeSystem: LinkSystem {
        var opened: [URL] = []
        /// The app that opens each scheme on this pretend Device.
        var apps = ["http": "Safari", "https": "Safari", "vscode": "Visual Studio Code"]
        var allows = false
        var asked: [String] = []
        var told: [String] = []
        func open(_ url: URL) { opened.append(url) }
        func appName(opening url: URL) -> String? { apps[url.scheme?.lowercased() ?? ""] }
        func confirm(_ question: String, detail: String) async -> Bool { asked.append(question); return allows }
        func tell(_ text: String) { told.append(text) }
    }

    let system = FakeSystem()
    let defaults: UserDefaults
    let clicks: LinkOpener

    init() {
        UserDefaults().removePersistentDomain(forName: "LinkOpenerTests")
        defaults = UserDefaults(suiteName: "LinkOpenerTests")!
        clicks = LinkOpener(system: system, defaults: defaults)
    }

    @Test func aWebLinkOpensInTheBrowser() async {
        #expect(await clicks.open("https://github.com/acme/api/pull/482", from: "Resources") == .opened)
        #expect(await clicks.open("http://localhost:4175/", from: "Resources") == .opened)
        #expect(system.opened.map(\.absoluteString) == ["https://github.com/acme/api/pull/482", "http://localhost:4175/"])
    }

    // MARK: Links to apps

    @Test func whetherALinkOpensAsksOrIsRefused() throws {
        let vscode = "Visual Studio Code"
        let table: [(link: String, allowed: Set<String>, app: String?, expected: LinkOpener.Decision)] = [
            ("https://github.com/acme/api/pull/482", [], "Safari", .open),
            ("HTTP://localhost:4175/", [], "Safari", .open),
            ("vscode://file/Users/dev/acme/api", [], vscode, .ask(app: vscode)),
            ("vscode://file/Users/dev/acme/api", ["vscode"], vscode, .open),
            ("VSCode://file/Users/dev/acme/api", ["vscode"], vscode, .open),
            ("vscode://file/Users/dev/acme/api", ["zed"], vscode, .ask(app: vscode)),
            ("zed://file/Users/dev/acme/api", [], nil, .noApp),
            ("vscode://file/Users/dev/acme/api", ["vscode"], nil, .noApp),
            ("file:///Users/dev/acme/api", ["file"], "Finder", .refuse),
            ("javascript:alert(1)", ["javascript"], "Safari", .refuse),
            ("data:text/html,hello", [], "Safari", .refuse),
        ]
        for row in table {
            let url = try #require(URL(string: row.link), "\(row.link)")
            #expect(LinkOpener.decide(url, allowed: row.allowed, app: row.app) == row.expected, "\(row.link)")
        }
    }

    let editor = "vscode://file/Users/dev/acme/api"

    @Test func theFirstAppLinkAsksNamingTheApp() async {
        #expect(await clicks.open(editor, from: "Resources") == .nothing)
        #expect(system.asked == ["Open links to Visual Studio Code from Resources?"])
        #expect(system.opened == [])
    }

    @Test func allowingOpensAndLaterLinksOpenWithoutAsking() async {
        system.allows = true
        #expect(await clicks.open(editor, from: "Resources") == .opened)
        #expect(await clicks.open("VSCODE://file/Users/dev/acme/web", from: "Resources") == .opened)
        #expect(system.asked.count == 1)
        #expect(system.opened.map(\.absoluteString) == ["vscode://file/Users/dev/acme/api", "VSCODE://file/Users/dev/acme/web"])

        // Remembered on this Device, not by this one LinkOpener.
        system.allows = false
        #expect(await LinkOpener(system: system, defaults: defaults).open(editor, from: "Resources") == .opened)
        #expect(system.asked.count == 1)
    }

    @Test func decliningOpensNothingAndAsksAgain() async {
        #expect(await clicks.open(editor, from: "Resources") == .nothing)
        #expect(await clicks.open(editor, from: "Resources") == .nothing)
        #expect(system.asked.count == 2)
        #expect(system.opened == [])
    }

    @Test func aSchemeNoAppOpensSaysSoInsteadOfAsking() async {
        #expect(await clicks.open("zed://file/Users/dev/acme/api", from: "Resources") == .nothing)
        #expect(system.asked == [])
        #expect(system.told == ["No app on this Device opens zed links."])
        #expect(system.opened == [])
    }

    @Test func aRefusedLinkNeverOpens() async {
        #expect(await clicks.open("file:///etc/passwd", from: "Resources") == .nothing)
        #expect(await clicks.open("javascript:alert(1)", from: "Resources") == .nothing)
        #expect(system.opened == [])
        #expect(system.asked == [])
    }

    // MARK: How many Tags fit before "+N"

    // Three Tags 50 wide with 4 between them; "+N" is 20 wide for any N.
    let three: [CGFloat] = [50, 50, 50]
    let more: [CGFloat] = [20, 20, 20]

    @Test func allShowWhenTheyFit() {
        #expect(TagRowRule.fit(three, more: more, in: 158, spacing: 4) == 3)
        #expect(TagRowRule.fit(three, more: more, in: 400, spacing: 4) == 3)
    }

    @Test func roomIsLeftForPlusN() {
        // Two Tags and "+1" take 50 + 4 + 50 + 4 + 20 = 128.
        #expect(TagRowRule.fit(three, more: more, in: 157, spacing: 4) == 2)
        #expect(TagRowRule.fit(three, more: more, in: 128, spacing: 4) == 2)
    }

    @Test func plusNCanPushOutAnotherTag() {
        // Two Tags and "+1" need 128; one Tag and "+2" need 74.
        #expect(TagRowRule.fit(three, more: more, in: 127, spacing: 4) == 1)
    }

    @Test func aWiderPlusNIsMeasuredForItsOwnCount() {
        // "+1" is narrow but "+2" is wide, so one Tag and "+2" (50 + 4 + 60) don't fit 100.
        #expect(TagRowRule.fit(three, more: [20, 60, 60], in: 100, spacing: 4) == 0)
        #expect(TagRowRule.fit(three, more: [20, 40, 60], in: 100, spacing: 4) == 1)
    }

    @Test func onlyPlusNWhenNoTagFits() {
        #expect(TagRowRule.fit(three, more: more, in: 30, spacing: 4) == 0)
    }

    @Test func noTagsShowsNothing() {
        #expect(TagRowRule.fit([], more: [], in: 100, spacing: 4) == 0)
    }
}
