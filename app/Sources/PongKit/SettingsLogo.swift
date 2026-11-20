import AppKit
import SwiftUI

/// The Pong logo at the head of Settings, from the brand kit: ink on the light window, paper on
/// the dark one. 120 pt wide, which keeps the bird in it well over
/// the guide's 16 px minimum.
struct SettingsLogo: View {
    @Environment(\.colorScheme) private var scheme

    var body: some View {
        if let image = brandResource(scheme == .dark ? "pong-logo-paper" : "pong-logo-ink", "svg").flatMap(NSImage.init(contentsOf:)) {
            Image(nsImage: image)
                .resizable()
                .aspectRatio(contentMode: .fit)
                .frame(width: 120)
                .accessibilityLabel("Pong")
        }
    }
}
