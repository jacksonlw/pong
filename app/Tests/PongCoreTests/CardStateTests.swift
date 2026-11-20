import Testing
import PongCore

@MainActor
struct CardStateTests {
    let several = Ping(id: "p", session: "s", question: "Which?", answers: [.init(label: "A"), .init(label: "B")],
                       allowSeveral: true, state: .waiting)

    @Test func tickedAnswersAndTypedTextGoAsOnePong() {
        let c = CardState()
        c.toggle("B", on: several); c.toggle("A", on: several)
        c.setDraft("and C", for: several)
        #expect(c.pong(for: several) == Ping.Pong(answers: ["A", "B"], text: "and C"))
        c.toggle("A", on: several)
        #expect(c.pong(for: several) == Ping.Pong(answers: ["B"], text: "and C"))
    }

    @Test func nothingTickedOrTypedSendsNothing() {
        let c = CardState()
        c.setDraft("   ", for: several)
        #expect(c.pong(for: several) == nil)
    }

    @Test func draftsStayOnTheirCard() {
        let c = CardState()
        c.setDraft("first", for: several)
        #expect(c.draft(for: several) == "first")
        #expect(c.draft(for: Ping(id: "other", session: "s", question: "?", state: .waiting)) == "")
    }

    @Test func outcomes() {
        let c = CardState()
        var p = several
        #expect(c.outcome(of: p) == nil)
        c.sent(Ping.Pong(answer: "A"), for: p)
        #expect(c.outcome(of: p) == .sending)
        p.state = .answered
        p.pong = Ping.Pong(answer: "A"); p.pong?.delivery = "sent"
        #expect(c.outcome(of: p) == .delivery("Sent, not yet delivered", withdrawable: true))
        p.pong?.delivery = "delivered"
        #expect(c.outcome(of: p) == .delivery("Delivered", withdrawable: false))
        p.pong = Ping.Pong(answer: "B"); p.pong?.delivery = "delivered"
        #expect(c.outcome(of: p) == .lost)
        c.refused("Pong could not reach that computer.", for: p)
        #expect(c.outcome(of: p) == .refused("Pong could not reach that computer."))
    }
}
