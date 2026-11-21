import AppKit
import SwiftUI

/// One file's part of a unified diff: its path, its lines with their headers, and how many lines
/// it adds and removes.
struct DiffFile: Equatable {
    var path: String
    var lines: [Substring]
    var added: Int
    var removed: Int

    /// A diff cut into its files, at each `diff --git` line, or where a `---` line is followed by
    /// a `+++` line when there are none. Lines before the first file go with it.
    static func split(_ source: String) -> [DiffFile] {
        let lines = splitLines(source)
        var starts: [Int] = lines.indices.filter { lines[$0].hasPrefix("diff ") }
        if starts.isEmpty {
            starts = lines.indices.filter { i in
                lines[i].hasPrefix("--- ") && i + 1 < lines.count && lines[i + 1].hasPrefix("+++ ")
            }
        }
        guard !starts.isEmpty else { return [] }
        starts[0] = 0
        return starts.indices.map { k in
            let part = Array(lines[starts[k]..<(k + 1 < starts.count ? starts[k + 1] : lines.count)])
            return DiffFile(path: path(of: part), lines: part,
                            added: part.filter { $0.hasPrefix("+") && !$0.hasPrefix("+++") }.count,
                            removed: part.filter { $0.hasPrefix("-") && !$0.hasPrefix("---") }.count)
        }
    }

    /// The file's path: from `+++ b/…`, or `--- a/…` for a deleted file, or the `diff --git` line.
    private static func path(of part: [Substring]) -> String {
        func strip(_ s: Substring) -> String {
            var p = s.split(separator: "\t").first.map(String.init) ?? String(s)
            if p.hasPrefix("a/") || p.hasPrefix("b/") { p.removeFirst(2) }
            return p
        }
        if let plus = part.first(where: { $0.hasPrefix("+++ ") }), !plus.dropFirst(4).hasPrefix("/dev/null") {
            return strip(plus.dropFirst(4))
        }
        if let minus = part.first(where: { $0.hasPrefix("--- ") }), !minus.dropFirst(4).hasPrefix("/dev/null") {
            return strip(minus.dropFirst(4))
        }
        if let git = part.first(where: { $0.hasPrefix("diff --git ") }), let b = git.range(of: " b/") {
            return String(git[b.upperBound...])
        }
        return "Change"
    }
}

/// A change that touches several files, one file at a time: a picker of its files above the
/// chosen one's diff, with the next and previous file a click away.
struct ChangeFilesView: View {
    var files: [DiffFile]
    @State private var chosen = 0

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack(spacing: 6) {
                IconButton(symbol: "chevron.left", label: "Previous file", help: "Previous file", enabled: chosen > 0) {
                    chosen = max(0, chosen - 1)
                }
                .disabled(chosen == 0)
                DiffFileMenu(files: files, chosen: $chosen)
                IconButton(symbol: "chevron.right", label: "Next file", help: "Next file", enabled: chosen < files.count - 1) {
                    chosen = min(files.count - 1, chosen + 1)
                }
                .disabled(chosen == files.count - 1)
                Text("\(chosen + 1) of \(files.count)")
                    .brandStyle(.secondary)
                    .monospacedDigit()
                    .foregroundStyle(Brand.secondary)
                    .fixedSize()
            }
            DiffView.lazy(files[chosen].lines).id(chosen)
        }
    }
}

/// The chosen file's path and counts; a click lists every file to choose from.
private struct DiffFileMenu: View {
    var files: [DiffFile]
    @Binding var chosen: Int
    @State private var hover = false

    var body: some View {
        Menu {
            ForEach(files.indices, id: \.self) { i in
                Button {
                    chosen = i
                } label: {
                    Text("\(files[i].path)   +\(files[i].added) −\(files[i].removed)")
                }
            }
        } label: {
            HStack(spacing: 8) {
                Image(systemName: "doc").imageScale(.large).font(Brand.Style.label.font).brandIcon()
                    .foregroundStyle(Brand.secondary)
                Text(files[chosen].path)
                    .brandStyle(.label)
                    .foregroundStyle(Brand.text)
                    .lineLimit(1)
                    .truncationMode(.middle)
                Spacer(minLength: 8)
                Text("+\(files[chosen].added)").brandStyle(.secondary).monospacedDigit().foregroundStyle(Brand.added)
                Text("−\(files[chosen].removed)").brandStyle(.secondary).monospacedDigit().foregroundStyle(Brand.removed)
                Image(systemName: "chevron.down").font(Brand.Style.secondary.font).foregroundStyle(Brand.secondary)
            }
            .padding(.horizontal, 10)
            .frame(minHeight: 30)
            .background(hover ? Brand.rowHover : Brand.row, in: Rectangle())
            .contentShape(Rectangle())
        }
        .menuStyle(.button)
        .buttonStyle(FocusedPlain())
        .menuIndicator(.hidden)
        .hoverHighlight($hover)
        .help("Choose a file")
        .accessibilityLabel("File \(chosen + 1) of \(files.count): \(files[chosen].path), \(files[chosen].added) added, \(files[chosen].removed) removed")
    }
}
