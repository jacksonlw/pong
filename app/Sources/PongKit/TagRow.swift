import SwiftUI

/// The row under the Project line: the Finished mark on a finished Ping, then its Tags. Nil
/// draws nothing. The card and the Thread both draw it.
struct TagLine: View {
    var ping: Ping

    /// Whether the Ping has anything for the row.
    static func shows(_ ping: Ping) -> Bool { ping.isFinished || !(ping.tags ?? []).isEmpty }

    var body: some View {
        HStack(spacing: TagRow.spacing) {
            if ping.isFinished { FinishedMark() }
            if let tags = ping.tags, !tags.isEmpty { TagRow(tags: tags) }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}

/// The green check on a finished Ping. It is Pong's own drawing, not a Tag: it is
/// not clickable, and voice mode does not read it.
struct FinishedMark: View {
    var body: some View {
        HStack(spacing: 4) {
            Image(systemName: "checkmark.circle.fill").imageScale(.large)
            Text("Finished").lineLimit(1)
        }
        .brandStyle(.label)
        .foregroundStyle(Brand.statusDone)
        .fixedSize()
        .frame(minHeight: 22)
        .accessibilityElement(children: .combine)
    }
}

/// A Ping's Tags in one row under the Project line, in the order the agent sent them: as many
/// as fit, then "+N", whose hover shows the rest. The card and the Thread both draw it.
struct TagRow: View {
    var tags: [Tag]

    static let spacing: CGFloat = 6

    var body: some View {
        // Every Tag and every possible "+N" are laid out; the row places the ones `fit` picks and
        // puts the rest outside its clip, so each is measured as SwiftUI draws it (`TagRowRule.fit`).
        Fitting(count: tags.count) {
            ForEach(tags.indices, id: \.self) { TagView(tag: tags[$0]) }
            ForEach(1..<(tags.count + 1), id: \.self) { n in
                MoreTags(rest: Array(tags.suffix(n))).accessibilityHidden(true)
            }
        }
        .clipped()
        .accessibilityElement(children: .contain)
        .accessibilityLabel("Tags")
    }

    /// Lays out `count` Tags followed by `count` "+N" views (for N = 1…count), placing the Tags
    /// that fit and the one "+N" that goes with them.
    private struct Fitting: Layout {
        var count: Int

        private func choose(_ subviews: Subviews, width: CGFloat?) -> (shown: Int, sizes: [CGSize]) {
            let sizes = subviews.map { $0.sizeThatFits(.unspecified) }
            let shown = TagRowRule.fit(sizes.prefix(count).map(\.width), more: sizes.dropFirst(count).map(\.width),
                                   in: width ?? .infinity, spacing: TagRow.spacing)
            return (shown, sizes)
        }

        /// The subviews drawn: the shown Tags, then the "+N" for the rest when there are any.
        private func placed(_ shown: Int) -> [Int] {
            Array(0..<shown) + (shown < count ? [count + (count - shown - 1)] : [])
        }

        func sizeThatFits(proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) -> CGSize {
            let (shown, sizes) = choose(subviews, width: proposal.width)
            let drawn = placed(shown).map { sizes[$0] }
            let width = drawn.map(\.width).reduce(0, +) + TagRow.spacing * CGFloat(max(drawn.count - 1, 0))
            return CGSize(width: width, height: drawn.map(\.height).max() ?? 0)
        }

        func placeSubviews(in bounds: CGRect, proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) {
            let (shown, sizes) = choose(subviews, width: proposal.width ?? bounds.width)
            let drawn = placed(shown)
            var x = bounds.minX
            for i in subviews.indices {
                if drawn.contains(i) {
                    subviews[i].place(at: CGPoint(x: x, y: bounds.midY), anchor: .leading, proposal: .unspecified)
                    x += sizes[i].width + TagRow.spacing
                } else {
                    // Outside the row's clip, so it is neither seen nor hovered.
                    subviews[i].place(at: CGPoint(x: bounds.minX - 10_000, y: bounds.midY), anchor: .leading, proposal: .unspecified)
                }
            }
        }
    }
}

/// One Tag: its icon and text on a badge, neutral or in its pastel, with square corners. It is
/// only read: it takes no click, no hover and no focus.
struct TagView: View {
    var tag: Tag

    var body: some View {
        let look = Self.look(tag.colour)
        HStack(spacing: 4) {
            // A step larger than the text's own small scale; the badge's fixed height keeps the Tag the same size.
            Image(systemName: Self.icon(tag.icon))
                .imageScale(.large)
            Text(tag.text)
                .lineLimit(1)
        }
        .brandStyle(.label)
        .foregroundStyle(look.text)
        .fixedSize()
        .padding(.horizontal, 6)
        .frame(height: TagView.height)
        .background(look.fill, in: Rectangle())
        .overlay { if let edge = look.edge { Rectangle().strokeBorder(edge, lineWidth: 1) } }
        .accessibilityElement(children: .combine)
    }

    /// The symbol drawn for a Tag's icon: its own when the brand kit's vocabulary has it, else the plain tag.
    static func icon(_ name: String?) -> String {
        guard let name, Brand.Tag.icons.contains(name) else { return Brand.Tag.fallbackIcon }
        return name
    }

    /// Every Tag's badge is this tall, whatever its icon.
    static let height: CGFloat = 22

    /// How a Tag is drawn: in its pastel's tint and outline with Tag text, or neutral (the
    /// secondary text colour on the row fill, with no outline) when it has no pastel the kit knows.
    struct Look: Equatable {
        var text: Color
        var fill: Color
        /// The outline, which only a coloured Tag has.
        var edge: Color?
    }

    static func look(_ colour: String?) -> Look {
        guard let pastel = colour.flatMap({ Brand.Tag.colour($0.lowercased()) }) else {
            return Look(text: Brand.secondary, fill: Brand.row, edge: nil)
        }
        return Look(text: Brand.tagText, fill: pastel.tint, edge: pastel.outline)
    }
}

/// "+N" for the Tags that don't fit; hovering it shows them in a popover, to read.
struct MoreTags: View {
    var rest: [Tag]
    @State private var hover = false

    var body: some View {
        Text("+\(rest.count)")
            .brandStyle(.label)
            .foregroundStyle(Brand.secondary)
            .fixedSize()
            .padding(.horizontal, 6)
            .frame(height: TagView.height)
            .background(hover ? Brand.rowHover : Brand.row, in: Rectangle())
            .hoverHighlight($hover)
            .popover(isPresented: $hover, arrowEdge: .bottom) {
                VStack(alignment: .leading, spacing: TagRow.spacing) {
                    ForEach(rest.indices, id: \.self) { TagView(tag: rest[$0]) }
                }
                .padding(10)
                .presentationBackground(Brand.surface)
            }
    }
}
