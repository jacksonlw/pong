import AppKit
import SwiftUI

/// A button drawn like the card's blocks: label type on a row-coloured block, which steps up
/// on hover and when pressed. `prominent` is the one primary action and takes the selected roles.
public struct BrandButtonStyle: ButtonStyle {
    var prominent: Bool
    public init(prominent: Bool = false) { self.prominent = prominent }

    public func makeBody(configuration: Configuration) -> some View {
        Drawn(configuration: configuration, prominent: prominent)
    }

    private struct Drawn: View {
        var configuration: Configuration
        var prominent: Bool
        @Environment(\.isEnabled) private var enabled
        @Environment(\.isFocused) private var focused
        @State private var hover = false
        private var lit: Bool { prominent && enabled }

        var body: some View {
            configuration.label
                .brandStyle(.label)
                .lineLimit(1)
                .fixedSize()
                .foregroundStyle(lit ? Brand.onSelected : enabled ? Brand.text : Brand.secondary)
                .padding(.horizontal, 12)
                .frame(minHeight: 28)
                .background(lit ? (configuration.isPressed || hover ? Brand.selectedHover : Brand.selected)
                            : (configuration.isPressed || hover) && enabled ? Brand.rowHover : Brand.row, in: Rectangle())
                .brandFocus(focused)
                .contentShape(Rectangle())
                .hoverHighlight($hover)
        }
    }
}

extension ButtonStyle where Self == BrandButtonStyle {
    /// The brand's button, as on the card.
    public static var brand: BrandButtonStyle { .init() }
    /// The window's one primary action, in the selected roles while it can act.
    public static var brandPrimary: BrandButtonStyle { .init(prominent: true) }
}

/// A toggle drawn as the card's checkbox: a row-coloured box that takes the selected roles when
/// on, with the shared focus outline while it has keyboard focus. VoiceOver hears a standard toggle.
public struct BrandToggleStyle: ToggleStyle {
    public init() {}

    public func makeBody(configuration: Configuration) -> some View {
        Drawn(configuration: configuration)
    }

    private struct Drawn: View {
        var configuration: Configuration
        @State private var hover = false

        var body: some View {
        Button { configuration.isOn.toggle() } label: {
            HStack(spacing: 10) {
                Box(on: configuration.isOn, hover: hover)
                configuration.label
            }
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .hoverHighlight($hover)
        .accessibilityRepresentation {
            Toggle(isOn: configuration.$isOn) { configuration.label }
        }
        }
    }

    private struct Box: View {
        var on: Bool
        var hover: Bool
        @Environment(\.isFocused) private var focused

        var body: some View {
            Image(systemName: "checkmark").fontWeight(Brand.iconWeight).imageScale(.large)
                .font(Brand.Style.label.font)
                .foregroundStyle(Brand.onSelected)
                .opacity(on ? 1 : 0)
                .frame(width: 20, height: 20)
                .background(on ? (hover ? Brand.selectedHover : Brand.selected) : hover ? Brand.rowHover : Brand.row, in: Rectangle())
                .brandFocus(focused)
        }
    }
}

/// One choice of a few, side by side; the chosen one takes the selected roles. Like the system's
/// segmented control it is one tab stop: the left and right arrows move the choice, and it shows
/// the shared focus outline while it has keyboard focus.
struct BrandSegments<Value: Hashable>: View {
    var options: [(Value, String)]
    @Binding var selection: Value
    @FocusState private var focused: Bool

    var body: some View {
        HStack(spacing: 4) {
            ForEach(options, id: \.0) { value, title in
                let chosen = value == selection
                HoverButton { selection = value } label: { hover in
                    Text(title)
                        .brandStyle(.label)
                        .foregroundStyle(chosen ? Brand.onSelected : Brand.text)
                        .padding(.horizontal, 10)
                        .frame(minHeight: 26)
                        .background(chosen ? Brand.selected : hover ? Brand.rowHover : .clear, in: Rectangle())
                        .contentShape(Rectangle())
                }
                .focusable(false)
                .accessibilityAddTraits(chosen ? .isSelected : [])
            }
        }
        .padding(3)
        .background(Brand.row, in: Rectangle())
        .brandFocus(focused)
        .focusable()
        .focused($focused)
        .focusEffectDisabled()
        .onKeyPress(.leftArrow) { step(-1) }
        .onKeyPress(.rightArrow) { step(1) }
        .accessibilityElement(children: .contain)
    }

    private func step(_ by: Int) -> KeyPress.Result {
        guard let i = options.firstIndex(where: { $0.0 == selection }), options.indices.contains(i + by) else { return .ignored }
        selection = options[i + by].0
        return .handled
    }
}

extension View {
    /// A text field drawn as the card's reply box: a row-coloured block with the shared focus
    /// outline while typing.
    func brandField(focused: Bool) -> some View {
        textFieldStyle(.plain)
            .foregroundStyle(Brand.text)
            .padding(.vertical, 8)
            .padding(.horizontal, 12)
            .background(Brand.row, in: Rectangle())
            .brandFocus(focused)
    }

    /// Sizes a Label in running text of a type role: its icon is drawn one step larger, in a
    /// square one line high, so the line is no taller than its text.
    func inlineIcon(_ style: Brand.Style) -> some View {
        labelStyle(InlineIconLabelStyle(line: style.size * style.lineHeight))
    }

    /// A field's placeholder in the secondary role, drawn by Pong: the system draws a field's
    /// own prompt in its placeholder grey, which reads as typed text on a row block.
    /// A symbol, when given, leads the placeholder and goes with it.
    func brandPlaceholder(_ text: String, symbol: String? = nil, shown: Bool) -> some View {
        overlay(alignment: .topLeading) {
            if shown {
                HStack(alignment: .firstTextBaseline, spacing: 8) {
                    if let symbol { Image(systemName: symbol).fontWeight(Brand.iconWeight).imageScale(.large).font(Brand.Style.label.font) }
                    Text(text)
                }
                .foregroundStyle(Brand.secondary)
                .allowsHitTesting(false)
                .accessibilityHidden(true)
            }
        }
        .accessibilityLabel(text)
    }

    /// Sizes a growing body-text field to the lines its text really takes, between the fewest
    /// and the most it may show. Left to itself, SwiftUI measures the field 4 pt narrower than
    /// the editor lays the text out, so near the end of a line it grows a line before the text
    /// wraps and shows an empty line below the last one.
    func brandFieldLines(_ text: String, _ lines: ClosedRange<Int>) -> some View {
        modifier(FieldLines(text: text, lines: lines))
    }

    /// The one outline Pong draws: keyboard focus, a 1 pt edge in the focus role with a faded
    /// 3 pt halo just outside it. Every focusable control uses it.
    public func brandFocus(_ focused: Bool) -> some View {
        modifier(FocusOutline(focused: focused))
    }
}

struct FieldLines: ViewModifier {
    var text: String
    var lines: ClosedRange<Int>
    @State private var width: CGFloat = 0

    func body(content: Content) -> some View {
        // The field will not be made shorter than it asks for, so it keeps its own height
        // and only its place in the layout is sized: its text starts at the top either way.
        content
            .frame(height: width > 0 ? Self.height(of: text, width: width, lines: lines) : nil, alignment: .top)
            .onGeometryChange(for: CGFloat.self, of: { $0.size.width }) { width = $0 }
            .background { FieldScrolling(text: text) }
    }

    /// The height of `text` laid out as the editor lays it out in a field this wide. The
    /// editor reaches 2 pt past the field on each side, which is the 4 pt SwiftUI leaves out.
    static func height(of text: String, width: CGFloat, lines: ClosedRange<Int>) -> CGFloat {
        let font = Brand.Style.bodyNSFont
        let storage = NSTextStorage(string: text, attributes: [.font: font])
        let layout = NSLayoutManager()
        let container = NSTextContainer(size: NSSize(width: width + 4, height: .greatestFiniteMagnitude))
        container.lineFragmentPadding = 2 // a field editor's
        layout.addTextContainer(container)
        storage.addLayoutManager(layout)
        layout.ensureLayout(for: container)
        let line = layout.defaultLineHeight(for: font)
        return min(max(layout.usedRect(for: container).height, CGFloat(lines.lowerBound) * line), CGFloat(lines.upperBound) * line)
    }
}

/// Scrolling for a growing field once its text is taller than the field. The field editor sits
/// in a clip view, not a scroll view, so it neither follows a caret Pong moves (Shift-Return in a
/// list) nor takes the wheel, which goes on to the list around it. This keeps the caret in view
/// whenever the text changes and scrolls the editor under the pointer by the wheel.
private struct FieldScrolling: NSViewRepresentable {
    var text: String

    func makeNSView(context: Context) -> Probe { Probe() }

    func updateNSView(_ v: Probe, context: Context) {
        guard v.text != text else { return }
        v.text = text
        // After SwiftUI has handed the editor the new text and caret.
        DispatchQueue.main.async { [weak v] in v?.revealCaret() }
    }

    final class Probe: NSView {
        var text = ""
        private var monitor: Any?

        override func hitTest(_ point: NSPoint) -> NSView? { nil }

        /// The field editor, when it is editing the field this probe sits behind.
        private var editor: NSTextView? {
            guard let window, let editor = window.firstResponder as? NSTextView, editor.isFieldEditor,
                  let field = editor.delegate as? NSTextField else { return nil }
            let here = convert(bounds, to: nil)
            return field.convert(field.bounds, to: nil).intersects(here) ? editor : nil
        }

        func revealCaret() {
            guard let editor else { return }
            editor.scrollRangeToVisible(editor.selectedRange())
        }

        override func viewDidMoveToWindow() {
            super.viewDidMoveToWindow()
            if let monitor { NSEvent.removeMonitor(monitor) }
            monitor = nil
            guard window != nil else { return }
            monitor = NSEvent.addLocalMonitorForEvents(matching: .scrollWheel) { [weak self] e in
                guard let self, self.scroll(e) else { return e }
                return nil
            }
        }

        /// Scrolls the editor by a wheel event over this field; false leaves the event to others.
        private func scroll(_ e: NSEvent) -> Bool {
            guard e.window === window, let editor,
                  convert(bounds, to: nil).contains(e.locationInWindow) else { return false }
            let visible = editor.visibleRect
            let most = editor.frame.height - visible.height
            guard most > 0.5 else { return false }
            let step = e.hasPreciseScrollingDeltas ? e.scrollingDeltaY : e.scrollingDeltaY * Brand.Style.bodyNSFont.pointSize
            let y = min(max(visible.minY - step, 0), most)
            editor.scroll(NSPoint(x: visible.minX, y: y))
            return true
        }
    }
}

private struct FocusOutline: ViewModifier {
    var focused: Bool
    @Environment(\.showsFocusEverywhere) private var everywhere

    func body(content: Content) -> some View {
        content.overlay {
            if focused || everywhere {
                ZStack {
                    Rectangle().strokeBorder(Brand.focus.opacity(0.2), lineWidth: 3).padding(-3)
                    Rectangle().strokeBorder(Brand.focus, lineWidth: 1)
                }
                .allowsHitTesting(false)
            }
        }
    }
}

extension EnvironmentValues {
    /// Snapshots set this to draw the focus outline on every control at once, since a rendered
    /// view cannot take keyboard focus.
    @Entry var showsFocusEverywhere = false
}

/// A plain button whose label is drawn from whether the pointer is over it, for a control with
/// no hover state of its own to keep.
struct HoverButton<Label: View>: View {
    var action: () -> Void
    @ViewBuilder var label: (Bool) -> Label
    @State private var hover = false

    var body: some View {
        Button(action: action) { label(hover) }
            .buttonStyle(FocusedPlain())
            .hoverHighlight($hover)
    }
}

/// A plain button that shows the shared focus outline while it has keyboard focus.
struct FocusedPlain: ButtonStyle {
    func makeBody(configuration: Configuration) -> some View { Drawn(configuration: configuration) }

    private struct Drawn: View {
        var configuration: Configuration
        @Environment(\.isFocused) private var focused
        var body: some View { configuration.label.brandFocus(focused) }
    }
}

/// A Label's icon centred in a square one line high beside its title; see `inlineIcon(_:)`.
struct InlineIconLabelStyle: LabelStyle {
    var line: CGFloat

    func makeBody(configuration: Configuration) -> some View {
        HStack(alignment: .top, spacing: 6) {
            Color.clear
                .frame(width: line, height: line)
                .overlay { configuration.icon.fontWeight(Brand.iconWeight).imageScale(.large) }
            configuration.title
        }
    }
}
