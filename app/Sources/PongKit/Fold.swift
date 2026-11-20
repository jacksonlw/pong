import SwiftUI

/// A question past six lines shows six and "Show more", which unfolds it in place.
/// Every Ping's question folds, the Plain fallback's included. The card draws it.
struct FoldedQuestion: View {
    var text: String
    @State private var unfolded: Bool

    init(text: String, unfolded: Bool = false) {
        self.text = text
        _unfolded = State(initialValue: unfolded)
    }

    var body: some View {
        Fold(unfolded: unfolded) {
            Text(text)
                .brandStyle(.question)
                .foregroundStyle(Brand.text)
                .fixedSize(horizontal: false, vertical: true)
            Text(text)
                .brandStyle(.question)
                .foregroundStyle(Brand.text)
                .lineLimit(Fold.lines)
                .accessibilityHidden(true)
            HoverButton { unfolded = true } label: { hover in
                Text("Show more")
                    .brandStyle(.label)
                    .foregroundStyle(hover ? Brand.secondary : Brand.link)
                    .underline()
            }
            .accessibilityHint("Shows the whole question")
        }
        .fixedSize(horizontal: false, vertical: true)
    }
}

/// Lays out a text whole, folded and "Show more" (in that order), placing the whole text when
/// it is no taller than the folded one or has been unfolded, else the folded text with "Show
/// more" beneath. The rest are placed far outside the window, unclipped so that no descender
/// of the drawn text is cut, and each is measured as SwiftUI draws it.
struct Fold: Layout {
    var unfolded: Bool
    static let lines = 6
    static let gap: CGFloat = 6

    private func folds(_ subviews: Subviews, width: CGFloat?) -> Bool {
        guard !unfolded, subviews.count == 3 else { return false }
        let p = ProposedViewSize(width: width, height: nil)
        return subviews[0].sizeThatFits(p).height > subviews[1].sizeThatFits(p).height + 0.5
    }

    func sizeThatFits(proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) -> CGSize {
        let p = ProposedViewSize(width: proposal.width, height: nil)
        let whole = subviews[0].sizeThatFits(p)
        guard folds(subviews, width: proposal.width) else { return CGSize(width: proposal.width ?? whole.width, height: whole.height) }
        let folded = subviews[1].sizeThatFits(p), more = subviews[2].sizeThatFits(.unspecified)
        return CGSize(width: proposal.width ?? folded.width, height: folded.height + Self.gap + more.height)
    }

    func placeSubviews(in bounds: CGRect, proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) {
        let p = ProposedViewSize(width: bounds.width, height: nil)
        let away = CGPoint(x: bounds.minX - 10_000, y: bounds.minY)
        let folding = folds(subviews, width: bounds.width)
        subviews[0].place(at: folding ? away : bounds.origin, proposal: p)
        subviews[1].place(at: folding ? bounds.origin : away, proposal: p)
        let y = bounds.minY + subviews[1].sizeThatFits(p).height + Self.gap
        subviews[2].place(at: folding ? CGPoint(x: bounds.minX, y: y) : away, proposal: .unspecified)
    }
}
