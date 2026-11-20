import SwiftUI

extension EnvironmentValues {
    /// What a click on a link Resource does; without one (as in snapshots) a click does nothing.
    @Entry public var linkOpener: LinkOpener?
}
