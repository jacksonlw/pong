import SwiftUI

/// Shown at the foot of the card while Pong listens, and only then: the level meter and the
/// stop button. Pong never listens without it.
struct ListeningBar: View {
    var level: Float
    var stop: () -> Void

    var body: some View {
        HStack(spacing: 10) {
            Image(systemName: "mic.fill").imageScale(.large)
                .font(Brand.Style.label.font)
                .brandIcon()
                .foregroundStyle(Brand.text)
                .accessibilityHidden(true)
            Text("Listening")
                .brandStyle(.label)
                .foregroundStyle(Brand.text)
            LevelMeter(level: level)
            Spacer(minLength: 0)
            Button("Stop", action: stop)
                .buttonStyle(.brand)
                .accessibilityLabel("Stop listening")
        }
        .padding(.horizontal, 16)
        .padding(.vertical, 8)
        .background(Brand.row, in: Rectangle())
        .accessibilityElement(children: .contain)
        .accessibilityLabel("Listening")
    }
}

/// Seven bars, lit from the left as the microphone's level rises.
struct LevelMeter: View {
    var level: Float
    static let bars = 7

    var body: some View {
        HStack(alignment: .center, spacing: 3) {
            ForEach(0..<Self.bars, id: \.self) { i in
                let lit = Float(i) < level * Float(Self.bars)
                Rectangle()
                    .fill(lit ? Brand.text : Brand.track)
                    .frame(width: 3, height: CGFloat(6 + 2 * min(i, Self.bars - 1 - i) + 4))
            }
        }
        .frame(height: 16)
        .accessibilityLabel("Microphone level")
        .accessibilityValue("\(Int(level * 100)) percent")
    }
}

/// Fills over three seconds before voice sends the understood answer.
struct CountdownBar: View {
    var progress: Double
    var paused: Bool

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            GeometryReader { g in
                ZStack(alignment: .leading) {
                    Rectangle().fill(Brand.track)
                    Rectangle().fill(Brand.text).frame(width: g.size.width * progress)
                }
            }
            .frame(height: 4)
            Text(paused ? "Waiting. Say \"go\" to send." : "Sending. Say \"wait\" or \"no\", or click the card.")
                .brandStyle(.secondary)
                .foregroundStyle(Brand.secondary)
        }
        .padding(.horizontal, 16)
        .padding(.bottom, 12)
        .accessibilityElement(children: .combine)
        .accessibilityValue("\(Int(progress * 100)) percent")
    }
}
