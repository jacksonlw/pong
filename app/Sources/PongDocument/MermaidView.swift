import SwiftUI
import WebKit

/// A Mermaid diagram drawn by the bundled mermaid.min.js in a web view as tall as the diagram.
/// The page takes its colours and type from tokens.css. A diagram Mermaid cannot read is shown
/// as `fallback`, which the app gives as the source in its code block.
public struct MermaidView<Fallback: View>: View {
    var source: String
    var fallback: Fallback
    @State private var height: CGFloat = 0
    @State private var failed = false

    public init(source: String, @ViewBuilder fallback: () -> Fallback) {
        self.source = source
        self.fallback = fallback()
    }

    public var body: some View {
        Group { content }
            // A changed source is drawn afresh, even where the last one could not be.
            .onChange(of: source) { height = 0; failed = false }
    }

    @ViewBuilder private var content: some View {
        if failed || mermaidPage == nil {
            fallback
        } else {
            MermaidWeb(source: source, height: $height, failed: $failed)
                .frame(maxWidth: .infinity)
                .frame(height: max(height, 1))
                .accessibilityLabel("Diagram")
                // A page that never says how tall it is shows the source rather than a blank strip.
                .task(id: source) {
                    try? await Task.sleep(for: .seconds(10))
                    if height == 0 { failed = true }
                }
        }
    }
}

/// The page: mermaid.html beside its script, tokens and font, in this target's bundle. In Pong.app
/// the bundle is in Contents/Resources, where `Bundle.module` does not look (and would crash);
/// under `swift test` and in the iPhone app it sits beside the code. Nil when it is nowhere, and
/// a diagram is then shown as its source.
let mermaidPage: URL? = {
    let places = [Bundle.main.resourceURL, Bundle.main.bundleURL, Bundle(for: PageAnchor.self).bundleURL.deletingLastPathComponent()]
    return places.lazy.compactMap { $0.flatMap { Bundle(url: $0.appendingPathComponent("Pong_PongDocument.bundle")) } }
        .compactMap { $0.url(forResource: "mermaid", withExtension: "html") }.first
}()

private final class PageAnchor {}

@MainActor final class MermaidCoordinator: NSObject, WKScriptMessageHandler, WKNavigationDelegate {
    var source = ""
    var shown = ""
    var loaded = false
    var height: Binding<CGFloat>
    var failed: Binding<Bool>

    init(height: Binding<CGFloat>, failed: Binding<Bool>) {
        self.height = height
        self.failed = failed
    }

    func makeWebView() -> WKWebView {
        let config = WKWebViewConfiguration()
        config.userContentController.add(WeakHandler(self), name: "pong")
        let web = MermaidWebView(frame: .zero, configuration: config)
        web.navigationDelegate = self
        if let page = mermaidPage { web.loadFileURL(page, allowingReadAccessTo: page.deletingLastPathComponent()) }
        return web
    }

    /// Draws the diagram once the page is up, and again only when its source changes.
    func show(in web: WKWebView) {
        guard loaded, shown != source else { return }
        shown = source
        web.callAsyncJavaScript("await show(text)", arguments: ["text": source], in: nil, in: .page) { _ in }
    }

    func webView(_ web: WKWebView, didFinish _: WKNavigation!) {
        loaded = true
        show(in: web)
    }

    /// The page is the only thing it loads: a link in a diagram goes nowhere.
    func webView(_: WKWebView, decidePolicyFor action: WKNavigationAction) async -> WKNavigationActionPolicy {
        action.request.url == mermaidPage ? .allow : .cancel
    }

    func userContentController(_: WKUserContentController, didReceive message: WKScriptMessage) {
        guard let body = message.body as? [String: Any] else { return }
        if let h = body["height"] as? Double { height.wrappedValue = CGFloat(h) }
        if body["error"] != nil { failed.wrappedValue = true }
    }
}

/// The content controller keeps its handler; this keeps it from keeping the coordinator.
final class WeakHandler: NSObject, WKScriptMessageHandler {
    weak var target: (any WKScriptMessageHandler)?
    init(_ target: any WKScriptMessageHandler) { self.target = target }
    func userContentController(_ c: WKUserContentController, didReceive m: WKScriptMessage) {
        target?.userContentController(c, didReceive: m)
    }
}

#if canImport(UIKit)
final class MermaidWebView: WKWebView {
    override init(frame: CGRect, configuration: WKWebViewConfiguration) {
        super.init(frame: frame, configuration: configuration)
        isOpaque = false
        backgroundColor = .clear
        scrollView.backgroundColor = .clear
        // The document scrolls; the diagram, as tall as itself, does not.
        scrollView.isScrollEnabled = false
    }
    required init?(coder: NSCoder) { fatalError() }
}

struct MermaidWeb: UIViewRepresentable {
    var source: String
    @Binding var height: CGFloat
    @Binding var failed: Bool

    func makeCoordinator() -> MermaidCoordinator { MermaidCoordinator(height: $height, failed: $failed) }
    func makeUIView(context: Context) -> WKWebView { context.coordinator.makeWebView() }
    func updateUIView(_ web: WKWebView, context: Context) {
        context.coordinator.source = source
        context.coordinator.show(in: web)
    }
}
#else
final class MermaidWebView: WKWebView {
    override init(frame: CGRect, configuration: WKWebViewConfiguration) {
        super.init(frame: frame, configuration: configuration)
        setValue(false, forKey: "drawsBackground")
    }
    required init?(coder: NSCoder) { fatalError() }
    // The document scrolls; the diagram, as tall as itself, passes the wheel on to it.
    override func scrollWheel(with event: NSEvent) { nextResponder?.scrollWheel(with: event) }
}

struct MermaidWeb: NSViewRepresentable {
    var source: String
    @Binding var height: CGFloat
    @Binding var failed: Bool

    func makeCoordinator() -> MermaidCoordinator { MermaidCoordinator(height: $height, failed: $failed) }
    func makeNSView(context: Context) -> WKWebView { context.coordinator.makeWebView() }
    func updateNSView(_ web: WKWebView, context: Context) {
        context.coordinator.source = source
        context.coordinator.show(in: web)
    }
}
#endif
