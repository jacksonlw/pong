// swift-tools-version:6.2
import PackageDescription

let package = Package(
    name: "Pong",
    platforms: [.macOS(.v15), .iOS(.v26)],
    // PongCore and PongDocument are libraries so other apps can build on them.
    products: [.library(name: "PongCore", targets: ["PongCore"]), .library(name: "PongDocument", targets: ["PongDocument"]),
               .library(name: "PongKit", targets: ["PongKit"]), .library(name: "PongApp", targets: ["PongApp"])],
    dependencies: [.package(url: "https://github.com/swiftlang/swift-markdown.git", exact: "0.9.0")],
    targets: [
        // Platform-neutral: the models, the Queue order, card state and what feeds them.
        .target(name: "PongCore"),
        // A document Resource read for both apps: markdown into blocks, code into highlighted runs,
        // and Mermaid drawn by the bundled mermaid.min.js. Its own target, so the iPhone's
        // notification extension does not carry the diagram script.
        .target(name: "PongDocument", dependencies: [.product(name: "Markdown", package: "swift-markdown")],
                resources: [.copy("Resources/mermaid.html"), .copy("Resources/mermaid.min.js"), .copy("Resources/mermaid-LICENSE.txt"),
                            .copy("Resources/tokens.css"), .copy("Resources/Inter-opsz-wght.ttf")]),
        // Engine API client, the model of what the Engine reports, and the card views.
        .target(name: "PongKit", dependencies: ["PongCore", "PongDocument"], resources: [.copy("Resources/Inter-opsz-wght.ttf"), .copy("Resources/ping-first.wav"), .copy("Resources/ping-follow-up.wav"), .copy("Resources/pong-mark-ink.svg"),
                                                    .copy("Resources/pong-logo-ink.svg"), .copy("Resources/pong-logo-paper.svg"),
                                                    .copy("Resources/harness-claude-code.svg"), .copy("Resources/harness-codex.svg")]),
        // AppKit glue: the card panel and the Bar's start panel, the Engine child process.
        .target(name: "PongApp", dependencies: ["PongKit"]),
        // The Mac app on Local: PongApp with no extras.
        .executableTarget(name: "Pong", dependencies: ["PongApp"]),
        .testTarget(name: "PongCoreTests", dependencies: ["PongCore"]),
        .testTarget(name: "PongDocumentTests", dependencies: ["PongDocument"]),
        .testTarget(
            name: "PongKitTests",
            dependencies: ["PongKit"],
            exclude: ["Fixtures", "Snapshots"]
        ),
    ],
    swiftLanguageModes: [.v5]
)
