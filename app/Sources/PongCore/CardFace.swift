import Foundation
import Observation

/// What the card's window shows: the card while a Ping waits, and the Bar while none does. The
/// Bar opens into the Threads list while the pointer rests on it, and into the start panel when
/// the human asks for a new Session.
public enum CardFace: Equatable, Sendable {
    case none, card, bar, threads, start
}

/// Decides the window's face from the card's scene, the pointer and the start panel. A Ping that
/// arrives while the human has the Threads list or the start panel open does not take it away
/// from them: it waits as a button at the top, and becomes the card when the pointer leaves the
/// list, the start panel closes, or the button is clicked. A card the human opened from the list
/// or the panel goes back to it when they leave the card; the start panel stays open aside,
/// with its draft, while the card shows. While a Thread the human opened is open, the list stays
/// open when the pointer leaves it, and opens again once the card goes.
@MainActor @Observable
public final class CardFaces {
    public private(set) var face: CardFace = .bar
    /// A Ping waits behind the open Threads list or start panel.
    public private(set) var held = false
    /// The Threads open in panels beside the card.
    public private(set) var openThreads: Set<SessionThread.SessionRef> = []

    private var scene = false
    private var pointing = false
    private var starting = false
    /// The human asked for the card: it shows as soon as it has a scene.
    private var asked = false
    /// Where the human opened the card from, to go back to when they leave it.
    private var back: CardFace?

    public init() {}

    /// The card has a scene to show (a Ping waits, or its next question is being written).
    public func set(scene: Bool) { self.scene = scene; decide() }
    /// The pointer rests on the Bar or the Threads list (the owner settles enters and exits).
    public func set(pointing: Bool) { self.pointing = pointing; decide() }
    /// The start panel is open in the Bar.
    public func set(starting: Bool) { self.starting = starting; decide() }
    /// The Threads open in panels beside the card changed.
    public func set(openThreads: Set<SessionThread.SessionRef>) { self.openThreads = openThreads; decide() }

    /// The human clicked the held Ping's button: the card shows it now.
    public func openHeld() {
        guard held else { return }
        openCard()
    }

    /// The human asked for the card (the Bar's waiting button): it shows once it has a scene,
    /// over the Threads list or the start panel too.
    public func openCard() {
        asked = true
        decide()
    }

    private func decide() {
        let was = face
        if scene && asked {
            asked = false
            back = was == .threads || was == .start ? was : nil
            face = .card
        } else if starting && back != .start {
            face = .start
        } else if scene {
            face = was == .threads && pointing ? .threads : .card
        } else if was == .card || was == .none {
            // Back to where the human opened the card from. Otherwise the last Ping answered
            // under the pointer: the card becomes the Bar, and the Threads list opens only on the
            // next time the pointer comes to it.
            switch back {
            case .start? where starting: face = .start
            case .threads?: pointing = true; face = .threads
            default: pointing = false; face = openThreads.isEmpty ? .bar : .threads
            }
            back = nil
        } else {
            face = pointing || !openThreads.isEmpty ? .threads : .bar
        }
        held = (face == .threads || face == .start) && scene
    }
}
