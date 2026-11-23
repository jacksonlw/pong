import PongKit

/// The Skill names every box on this Mac offers, loaded from the Engine; set as the app starts.
@MainActor
enum SharedSkills {
    static var names = SkillNames(fetch: { _ in nil })
}
