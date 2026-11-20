import Foundation

/// A stretch of code and what it is; `kind` is nil for plain text.
public struct CodeRun: Equatable, Sendable {
    public enum Kind: Equatable, Sendable { case keyword, string, number, comment }
    public var text: String
    public var kind: Kind?
}

/// A small highlighter for the languages agents write most: comments, strings, numbers and
/// keywords, which is most of what makes code easier to read. A language it does not know is left plain.
public enum Highlight {
    struct Language {
        var lineComments: [String]
        var blockComment: (String, String)?
        var quotes: Set<Character>
        var keywords: Set<String>
        var anyCase = false
        var tripleQuotes = false
    }

    static let cLike = (["//"], ("/*", "*/") as (String, String)?)
    static func words(_ s: String) -> Set<String> { Set(s.split(separator: " ").map(String.init)) }

    static let languages: [String: Language] = {
        let swift = Language(lineComments: cLike.0, blockComment: cLike.1, quotes: ["\""], keywords: words(
            "let var func if else guard return struct class enum case switch default for in while repeat import protocol extension init self Self try catch throw throws rethrows async await nil true false public private internal fileprivate open static some any where defer do break continue typealias inout weak lazy override final mutating is as"))
        let go = Language(lineComments: cLike.0, blockComment: cLike.1, quotes: ["\"", "'", "`"], keywords: words(
            "package import func var const type struct interface map chan go defer return if else for range switch case default break continue select nil true false fallthrough goto"))
        let js = Language(lineComments: cLike.0, blockComment: cLike.1, quotes: ["\"", "'", "`"], keywords: words(
            "const let var function return if else for while do switch case default break continue new class extends import export from as async await try catch finally throw typeof instanceof in of null undefined true false this interface type enum implements yield void readonly"))
        let rust = Language(lineComments: cLike.0, blockComment: cLike.1, quotes: ["\""], keywords: words(
            "fn let mut pub struct enum impl trait use mod match if else for while loop return self Self crate super as where const static async await move ref true false dyn unsafe in"))
        let c = Language(lineComments: cLike.0, blockComment: cLike.1, quotes: ["\"", "'"], keywords: words(
            "if else for while do switch case default break continue return class struct public private protected static void int char float double long bool boolean true false null nullptr new this const enum try catch throw fun val var import package interface namespace using include typedef unsigned override"))
        let python = Language(lineComments: ["#"], quotes: ["\"", "'"], keywords: words(
            "def class return if elif else for while in not and or is import from as with try except finally raise pass break continue lambda yield None True False async await global nonlocal"), tripleQuotes: true)
        let shell = Language(lineComments: ["#"], quotes: ["\"", "'"], keywords: words(
            "if then else elif fi for while until do done case esac in function return export local exit set unset source"))
        let ruby = Language(lineComments: ["#"], quotes: ["\"", "'"], keywords: words(
            "def end class module if elsif else unless while until do return nil true false require yield self then"))
        let yaml = Language(lineComments: ["#"], quotes: ["\"", "'"], keywords: words("true false null yes no"))
        let sql = Language(lineComments: ["--"], blockComment: cLike.1, quotes: ["'", "\""], keywords: words(
            "select from where and or not insert into values update set delete create table alter drop index join left right inner outer on group by order having limit as null is in distinct union all primary key references begin commit returning with case when then else end"), anyCase: true)
        let json = Language(lineComments: [], quotes: ["\""], keywords: words("true false null"))
        let css = Language(lineComments: [], blockComment: cLike.1, quotes: ["\"", "'"], keywords: [])
        let aliases: [(Language, [String])] = [
            (swift, ["swift"]), (go, ["go", "golang"]),
            (js, ["js", "javascript", "jsx", "ts", "typescript", "tsx", "mjs"]), (rust, ["rust", "rs"]),
            (c, ["c", "h", "cpp", "c++", "cc", "java", "kotlin", "kt", "cs", "csharp", "objc", "dart", "scala"]),
            (python, ["python", "py"]), (shell, ["bash", "sh", "zsh", "shell", "console", "fish"]),
            (ruby, ["ruby", "rb"]), (yaml, ["yaml", "yml", "toml"]), (sql, ["sql"]),
            (json, ["json", "jsonc"]), (css, ["css", "scss"]),
        ]
        return Dictionary(aliases.flatMap { l, names in names.map { ($0, l) } }, uniquingKeysWith: { a, _ in a })
    }()

    public static func runs(_ code: String, language: String?) -> [CodeRun] {
        guard let name = language?.lowercased(), let lang = languages[name] else { return [CodeRun(text: code, kind: nil)] }
        let s = Array(code)
        var out: [CodeRun] = []
        func emit(_ text: String, _ kind: CodeRun.Kind?) {
            if kind == nil, let last = out.last, last.kind == nil { out[out.count - 1].text += text } else { out.append(CodeRun(text: text, kind: kind)) }
        }
        func starts(_ token: String, at i: Int) -> Bool {
            let t = Array(token)
            return i + t.count <= s.count && Array(s[i..<i + t.count]) == t
        }
        func isWord(_ c: Character) -> Bool { c.isLetter || c.isNumber || c == "_" }
        var i = 0
        while i < s.count {
            let c = s[i]
            if let (open, close) = lang.blockComment, starts(open, at: i) {
                var j = i + open.count
                while j < s.count, !starts(close, at: j) { j += 1 }
                j = min(s.count, j + close.count)
                emit(String(s[i..<j]), .comment); i = j; continue
            }
            // A # starts a comment only at the start of a line or after a space, so $# and a#b stay code.
            if let lc = lang.lineComments.first(where: { starts($0, at: i) }),
               lc != "#" || i == 0 || s[i - 1].isWhitespace {
                var j = i
                while j < s.count, s[j] != "\n" { j += 1 }
                emit(String(s[i..<j]), .comment); i = j; continue
            }
            // Python's triple-quoted strings run across lines to the same three quotes.
            if lang.tripleQuotes, lang.quotes.contains(c), starts(String(repeating: c, count: 3), at: i) {
                let fence = String(repeating: c, count: 3)
                var j = i + 3
                while j < s.count, !starts(fence, at: j) { j += s[j] == "\\" ? 2 : 1 }
                j = min(s.count, j + 3)
                emit(String(s[i..<j]), .string); i = j; continue
            }
            if lang.quotes.contains(c) {
                var j = i + 1
                while j < s.count, s[j] != c, c == "`" || s[j] != "\n" { j += s[j] == "\\" ? 2 : 1 }
                // A quote left open ends at its line, leaving the newline to the code after it.
                if j < s.count, s[j] == c { j += 1 }
                j = min(s.count, j)
                emit(String(s[i..<j]), .string); i = j; continue
            }
            if c.isNumber, i == 0 || !isWord(s[i - 1]) {
                var j = i
                while j < s.count, isWord(s[j]) || s[j] == "." { j += 1 }
                emit(String(s[i..<j]), .number); i = j; continue
            }
            if c.isLetter || c == "_" {
                var j = i
                while j < s.count, isWord(s[j]) { j += 1 }
                let word = String(s[i..<j])
                emit(word, lang.keywords.contains(lang.anyCase ? word.lowercased() : word) ? .keyword : nil); i = j; continue
            }
            emit(String(c), nil); i += 1
        }
        return out
    }
}
