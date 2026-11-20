import Testing
import PongCore

struct ListTextTests {
    @Test func bulletsAndNewLines() {
        #expect(ListText.bulleted("- one\n  * two\nthree - four\n-five") == "• one\n  • two\nthree - four\n-five")
        #expect(ListText.markdown("• one\n  • two") == "- one\n  - two")
        func newLine(_ s: String, at offset: Int? = nil) -> (String, Int) {
            let i = s.index(s.startIndex, offsetBy: offset ?? s.count)
            let (out, at) = ListText.newLine(in: s, at: i..<i)
            return (out, out.distance(from: out.startIndex, to: at))
        }
        #expect(newLine("plain") == ("plain\n", 6))
        #expect(newLine("ab", at: 1) == ("a\nb", 2))
        #expect(newLine("• one") == ("• one\n• ", 8), "the list goes on")
        #expect(newLine("• one\n• ") == ("• one\n", 6), "an empty item ends the list")
    }
}
