package turn

import (
	"net/url"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/jacksonlw/pong/internal/engine"
	"github.com/jacksonlw/pong/internal/harness"
)

// Facts are what Pong reads from the turn itself, beside its closing
// message, when it builds the turn's Ping. Each Harness adapter fills what
// its transcript gives; all may be empty.
type Facts struct {
	TestOutput string   // the output of the last command in the turn that ran tests
	Documents  []string // Markdown or text files the turn wrote, absolute paths
}

// Build turns an agent's closing message into a Ping, for a turn that ended
// without one of the agent's own, so the agent is never told to write one. It
// reads only the message's
// structure, with no model:
//
//   - the question is the last sentence that asks, or offers to do more;
//   - a list beside it gives the answers, the one marked as recommended first;
//   - code blocks, tables and links become Resources, titled by the line that
//     introduces them, and so do the turn's last test output and the documents
//     it wrote;
//   - the prose that is left is the context.
//
// A message that asks nothing gives a Finished Ping, whose first sentence says
// what was done. The Ping is Captured, so the Engine takes it as it comes.
func Build(message string, f Facts) engine.NewPing {
	blocks := parse(message)
	var n engine.NewPing
	n.Captured = true
	var context []string
	used := make([]bool, len(blocks))

	// Resources from the message: code, tables, links.
	for i, b := range blocks {
		switch b.kind {
		case blockCode:
			if strings.TrimSpace(b.text) == "" {
				used[i] = true
				continue
			}
			title := titleFrom(intro(blocks, i), "Code")
			kind := engine.KindText
			if looksLikeOutput(intro(blocks, i)) {
				kind = engine.KindOutput
			}
			n.Resources = append(n.Resources, engine.NewResource{Title: title, Kind: kind, Content: b.text})
			used[i] = true
		case blockTable:
			title := titleFrom(intro(blocks, i), "Table")
			n.Resources = append(n.Resources, engine.NewResource{Title: title, Kind: engine.KindSheet, Content: tableCSV(b.lines)})
			used[i] = true
		}
	}

	// The question, and the list beside it as answers.
	q := -1
	for i := len(blocks) - 1; i >= 0 && q < 0; i-- {
		if blocks[i].kind == blockPara && asks(blocks[i].text) {
			q = i
		}
	}
	if q >= 0 {
		n.Question = askingSentence(blocks[q].text)
		n.BlocksAgent = true
		n.Tags = []engine.Tag{harness.TagQuestion}
		used[q] = q >= 0 && strings.TrimSpace(blocks[q].text) == n.Question
		if rest := strings.TrimSpace(strings.Replace(blocks[q].text, n.Question, "", 1)); rest != "" {
			blocks[q].text = rest
		}
		// Only a list after the question offers its answers: one before it
		// is the report the question follows.
		if j := q + 1; j < len(blocks) && blocks[j].kind == blockList {
			n.Answers, n.Recommended = answers(blocks[j].lines)
			used[j] = len(n.Answers) > 0
		}
		if len(n.Answers) == 0 && offers(n.Question) {
			n.Answers = []engine.Answer{{Label: "Yes"}, {Label: "No"}}
		}
	} else {
		n.Finished = true
		n.Question = firstSentence(message)
	}
	// A sentence that introduces a list ends in a colon; as a question it
	// ends as a sentence does, and the list stays in the context.
	if q := strings.TrimSpace(n.Question); strings.HasSuffix(q, ":") {
		n.Question = strings.TrimSuffix(q, ":") + "."
	}

	// The context is the prose and lists left over; a Finished Ping's first
	// sentence is already its question.
	for i, b := range blocks {
		if used[i] || (b.kind != blockPara && b.kind != blockList) {
			continue
		}
		t := strings.TrimSpace(b.text)
		if n.Finished {
			for _, q := range []string{n.Question, strings.TrimSuffix(n.Question, ".") + ":"} {
				if strings.HasPrefix(t, q) {
					t = strings.TrimSpace(t[len(q):])
					break
				}
			}
		}
		if t != "" {
			context = append(context, t)
		}
	}
	n.Context = strings.Join(context, "\n\n")

	// Links in the prose.
	seen := map[string]bool{}
	for _, u := range linkRE.FindAllString(message, -1) {
		u = strings.TrimRight(u, ".,;:)]'\"`")
		if seen[u] || inCode(blocks, u) {
			continue
		}
		seen[u] = true
		n.Resources = append(n.Resources, engine.NewResource{Title: linkTitle(u), Kind: engine.KindLink, Content: u})
	}

	// What the turn itself left: its last test output and its documents.
	if out := strings.TrimSpace(f.TestOutput); out != "" {
		n.Resources = append(n.Resources, engine.NewResource{Title: "Test output", Kind: engine.KindOutput, Content: out})
	}
	for _, d := range f.Documents {
		name := strings.NewReplacer("-", " ", "_", " ").Replace(strings.TrimSuffix(filepath.Base(d), filepath.Ext(d)))
		n.Resources = append(n.Resources, engine.NewResource{Title: titleFrom(name, "Document"), Kind: engine.KindDocument, File: d})
	}

	if strings.TrimSpace(n.Question) == "" {
		n.Question = "The agent finished its turn without a message."
	}
	for i := range n.Resources {
		n.Resources[i].Title = fitTitle(n.Resources[i].Title, n.Resources[i].Kind)
	}
	return n
}

// kindNoun is the word a short title is filled out with, so a Resource's
// title is never a single word, which the Engine refuses.
var kindNoun = map[string]string{
	engine.KindText: "code", engine.KindOutput: "output", engine.KindSheet: "table",
	engine.KindLink: "link", engine.KindDocument: "document",
}

// fitTitle makes title two to four words, as the Engine takes a Resource's.
func fitTitle(title, kind string) string {
	ws := strings.Fields(title)
	if len(ws) > 4 {
		ws = ws[:4]
	}
	noun := kindNoun[kind]
	if noun == "" {
		noun = "file"
	}
	switch len(ws) {
	case 0:
		ws = []string{strings.ToUpper(noun[:1]) + noun[1:], "attached"}
	case 1:
		if strings.EqualFold(ws[0], noun) {
			ws = append([]string{"The"}, ws...)
		} else {
			ws = append(ws, noun)
		}
	}
	return strings.Join(ws, " ")
}

type blockKind int

const (
	blockPara blockKind = iota
	blockList
	blockCode
	blockTable
	blockHeading
)

type block struct {
	kind  blockKind
	text  string   // the block's text; for code, the code without its fences
	lines []string // a list's items or a table's rows
}

var (
	listItemRE = regexp.MustCompile(`^\s*(?:[-*+]|\d+[.)])\s+(.*)$`)
	linkRE     = regexp.MustCompile(`https?://[^\s<>()\[\]` + "`" + `]+`)
)

// parse splits Markdown into paragraphs, lists, fenced code, tables and
// headings. It is no full Markdown parser: it knows what agents write.
func parse(s string) []block {
	var out []block
	lines := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	var para []string
	flush := func() {
		if len(para) > 0 {
			out = append(out, block{kind: blockPara, text: strings.Join(para, " ")})
			para = nil
		}
	}
	for i := 0; i < len(lines); i++ {
		l := lines[i]
		t := strings.TrimSpace(l)
		switch {
		case strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~"):
			flush()
			fence := t[:3]
			var code []string
			for i++; i < len(lines) && !strings.HasPrefix(strings.TrimSpace(lines[i]), fence); i++ {
				code = append(code, lines[i])
			}
			out = append(out, block{kind: blockCode, text: strings.Join(code, "\n")})
		case strings.HasPrefix(t, "|"):
			flush()
			var rows []string
			for ; i < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[i]), "|"); i++ {
				rows = append(rows, strings.TrimSpace(lines[i]))
			}
			i--
			out = append(out, block{kind: blockTable, lines: rows, text: strings.Join(rows, "\n")})
		case listItemRE.MatchString(l):
			flush()
			var items, raw []string
			for ; i < len(lines); i++ {
				if m := listItemRE.FindStringSubmatch(lines[i]); m != nil && !indented(lines[i]) {
					items = append(items, m[1])
					raw = append(raw, strings.TrimSpace(lines[i]))
				} else if strings.TrimSpace(lines[i]) != "" && indented(lines[i]) && len(items) > 0 {
					items[len(items)-1] += " " + strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(lines[i]), "-*+ "))
					raw = append(raw, lines[i])
				} else {
					break
				}
			}
			i--
			out = append(out, block{kind: blockList, lines: items, text: strings.Join(raw, "\n")})
		case strings.HasPrefix(t, "#"):
			flush()
			out = append(out, block{kind: blockHeading, text: strings.TrimSpace(strings.TrimLeft(t, "#"))})
		case t == "":
			flush()
		default:
			para = append(para, t)
		}
	}
	flush()
	return out
}

func indented(l string) bool { return strings.HasPrefix(l, "  ") || strings.HasPrefix(l, "\t") }

// intro is the line that introduces block i: the end of the paragraph or the
// heading right before it.
func intro(bs []block, i int) string {
	if i == 0 {
		return ""
	}
	p := bs[i-1]
	if p.kind != blockPara && p.kind != blockHeading {
		return ""
	}
	s := strings.TrimSpace(p.text)
	if k := strings.LastIndexFunc(strings.TrimRight(s, ".:"), endsSentence); k >= 0 {
		s = s[k+1:]
	}
	return strings.TrimSpace(s)
}

var wordRE = regexp.MustCompile(`[\p{L}\p{N}][\p{L}\p{N}'’._/-]*`)

// titleFrom makes a Resource title of at most four words from s.
func titleFrom(s, fallback string) string {
	ws := wordRE.FindAllString(strings.NewReplacer("`", "", "*", "", "_", " ").Replace(s), -1)
	var keep []string
	for _, w := range ws {
		if len(keep) == 0 && filler[strings.ToLower(w)] {
			continue
		}
		keep = append(keep, strings.Trim(w, "._/-"))
		if len(keep) == 4 {
			break
		}
	}
	if len(keep) == 0 {
		return fallback
	}
	t := strings.Join(keep, " ")
	return strings.ToUpper(t[:1]) + t[1:]
}

var filler = map[string]bool{"here": true, "here's": true, "here’s": true, "this": true, "is": true, "the": true, "a": true, "an": true, "and": true, "it": true, "i": true, "below": true}

func looksLikeOutput(s string) bool {
	s = strings.ToLower(s)
	for _, w := range []string{"output", "result", "log", "printed", "shows", "showed", "error", "ran"} {
		if strings.Contains(s, w) {
			return true
		}
	}
	return false
}

// tableCSV turns a Markdown table's rows into comma-separated lines, dropping
// the line under the header.
func tableCSV(rows []string) string {
	var out []string
	for _, r := range rows {
		r = strings.Trim(strings.TrimSpace(r), "|")
		cells := strings.Split(r, "|")
		sep := true
		for i, c := range cells {
			c = strings.TrimSpace(c)
			if strings.Trim(c, ":- ") != "" {
				sep = false
			}
			c = strings.NewReplacer("`", "", "**", "").Replace(c)
			if strings.ContainsAny(c, ",\"\n") {
				c = `"` + strings.ReplaceAll(c, `"`, `""`) + `"`
			}
			cells[i] = c
		}
		if sep {
			continue
		}
		out = append(out, strings.Join(cells, ","))
	}
	return strings.Join(out, "\n")
}

var offerRE = regexp.MustCompile(`(?i)\b(want me to|should i|shall i|do you want|would you like|would you prefer|if you'd like|if you’d like|if you want me to|which (?:one|would)|ok to|okay to)\b`)

// asks reports whether a paragraph asks the human something. A question mark
// inside code or a link asks nothing.
func asks(s string) bool {
	s = strings.TrimSpace(linkRE.ReplaceAllString(stripCode(s), ""))
	return strings.Contains(s, "?") || offerRE.MatchString(s)
}

// offers reports whether a question offers to do something, so Yes and No
// answer it.
func offers(q string) bool {
	return offerRE.MatchString(q) && !strings.Contains(strings.ToLower(q), " or ")
}

var inlineCodeRE = regexp.MustCompile("`[^`]*`")

func stripCode(s string) string { return inlineCodeRE.ReplaceAllString(s, "") }

// askingSentence is the last sentence in s that asks.
func askingSentence(s string) string {
	ss := sentences(s)
	for i := len(ss) - 1; i >= 0; i-- {
		if asks(ss[i]) {
			return ss[i]
		}
	}
	return strings.TrimSpace(s)
}

// endsSentence reports whether r ends a sentence: a full stop, a question
// mark or an exclamation mark.
func endsSentence(r rune) bool { return r == '.' || r == '?' || r == 0x21 }

// sentences splits s where a sentence ends, keeping inline code whole.
func sentences(s string) []string {
	var out []string
	var cur strings.Builder
	inCode := false
	rs := []rune(s)
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		cur.WriteRune(r)
		if r == '`' {
			inCode = !inCode
		}
		if inCode || !endsSentence(r) {
			continue
		}
		// A sentence may end inside emphasis, brackets or quotes: take those
		// closing marks with it, then it must be followed by a space.
		j := i + 1
		for j < len(rs) && strings.ContainsRune("*_)]\"'”’", rs[j]) {
			j++
		}
		if j < len(rs) && rs[j] != ' ' {
			continue
		}
		for ; i+1 < j; i++ {
			cur.WriteRune(rs[i+1])
		}
		if t := strings.TrimSpace(cur.String()); t != "" {
			out = append(out, t)
		}
		cur.Reset()
	}
	if t := strings.TrimSpace(cur.String()); t != "" {
		out = append(out, t)
	}
	return out
}

func firstSentence(s string) string {
	for _, b := range parse(s) {
		if b.kind == blockPara {
			if ss := sentences(b.text); len(ss) > 0 {
				return ss[0]
			}
		}
	}
	return strings.TrimSpace(s)
}

var recommendRE = regexp.MustCompile(`(?i)\s*[(\[]?\s*(?:my pick|recommended|my recommendation|i recommend this|i'd pick this|suggested)\s*[)\]]?\s*`)

// answers reads a list as answers: each item's label is its bold part, or
// what comes before a colon or dash, or the item itself; the rest is where it
// leads. An item marked as recommended is the recommended one.
func answers(items []string) (out []engine.Answer, recommended string) {
	if len(items) < 2 || len(items) > 6 {
		return nil, ""
	}
	for _, it := range items {
		rec := recommendRE.MatchString(it)
		it = strings.TrimSpace(recommendRE.ReplaceAllString(it, " "))
		label, rest := it, ""
		if m := boldRE.FindStringSubmatchIndex(it); m != nil && m[0] == 0 {
			label, rest = it[m[2]:m[3]], it[m[1]:]
		} else if k := strings.IndexAny(it, ":—–"); k > 0 && k < 80 {
			label, rest = it[:k], it[k:]
		}
		label = strings.Trim(strings.TrimSpace(strings.ReplaceAll(label, "`", "")), ".:")
		rest = strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(rest), ":—–-"))
		if label == "" {
			return nil, ""
		}
		if w := strings.Fields(label); len(w) > engine.MaxLabelWords {
			rest = strings.TrimSpace(strings.Join(w[engine.MaxLabelWords:], " ") + " " + rest)
			label = strings.Join(w[:engine.MaxLabelWords], " ")
		}
		out = append(out, engine.Answer{Label: label, LeadsTo: strings.TrimRight(rest, ".")})
		if rec && recommended == "" {
			recommended = label
		}
	}
	if recommended != "" {
		for i, a := range out {
			if a.Label == recommended && i > 0 {
				out = append([]engine.Answer{a}, append(out[:i:i], out[i+1:]...)...)
				break
			}
		}
	}
	if len(out) > 4 {
		out = out[:4]
	}
	return out, recommended
}

var boldRE = regexp.MustCompile(`\*\*([^*]+)\*\*`)

func inCode(bs []block, s string) bool {
	for _, b := range bs {
		if b.kind == blockCode && strings.Contains(b.text, s) {
			return true
		}
	}
	return false
}

func linkTitle(u string) string {
	p, err := url.Parse(u)
	if err != nil || p.Host == "" {
		return "Link"
	}
	host := strings.TrimPrefix(p.Host, "www.")
	if seg := strings.Trim(p.Path, "/"); seg != "" {
		parts := strings.Split(seg, "/")
		return titleFrom(host+" "+parts[len(parts)-1], "Link")
	}
	return host
}
