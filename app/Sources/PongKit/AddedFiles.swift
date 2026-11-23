import AppKit
import PongCore
import SwiftUI

/// The Files added to a reply, under its box: one row each, removed with its ×, and why one
/// more could not be added.
struct AddedFiles: View {
    var paths: [String]
    var problem: String?
    var remove: (String) -> Void

    var body: some View {
        if !paths.isEmpty || problem != nil {
            VStack(alignment: .leading, spacing: 4) {
                ForEach(paths, id: \.self) { p in
                    StartPanelView.FileRow(path: p) { remove(p) }
                }
                if let problem {
                    Label(problem, systemImage: "exclamationmark.circle").inlineIcon(.secondary)
                        .brandStyle(.secondary)
                        .foregroundStyle(Brand.text)
                        .fixedSize(horizontal: false, vertical: true)
                }
            }
        }
    }
}

/// What a box does with Files pasted or dropped on it: adds them to its draft, unless they would
/// take the reply past the limit.
enum DraftFiles {
    /// The paths after adding `new`, and why some were not added.
    static func adding(_ new: [String], to paths: [String]) -> (paths: [String], problem: String?) {
        var out = paths
        for p in new where !out.contains(p) { out.append(p) }
        if let problem = PongFile.problem(with: out) { return (paths, problem) }
        return (out, nil)
    }

    /// The Files on a pasteboard: file URLs as they are, an image as a PNG kept in the drafts
    /// folder. Empty when it holds neither, so the text view pastes text as usual.
    static func from(_ pb: NSPasteboard) -> [String] {
        if let urls = pb.readObjects(forClasses: [NSURL.self], options: [.urlReadingFileURLsOnly: true]) as? [URL], !urls.isEmpty {
            return urls.filter { !$0.hasDirectoryPath }.map(\.path)
        }
        guard pb.string(forType: .string) == nil, let image = NSImage(pasteboard: pb),
              let tiff = image.tiffRepresentation, let png = NSBitmapImageRep(data: tiff)?.representation(using: .png, properties: [:]),
              let path = try? PongFile.keep(png, ext: "png") else { return [] }
        return [path]
    }
}
