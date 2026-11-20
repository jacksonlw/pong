package engine

import (
	"fmt"
	"net/url"
	"strings"
	"unicode/utf8"
)

// Tag is short text with an icon, and optionally a colour, that an agent puts
// on a Ping or Note to say what kind of Ping it is, where its change lands or
// what it concerns. A Tag is never clicked: a link travels as a link Resource. The icon is stored as given, and so is the colour on a Ping with
// no Project; in a Project only a pastel counts (see keepTagColours). Drawing
// an unknown icon or colour plainly is the card's job.
type Tag struct {
	Text   string `json:"text"`
	Icon   string `json:"icon,omitempty"`
	Colour string `json:"colour,omitempty"`
	// Link is read only so a Tag sent with one, as agents did before, is
	// refused with a fix-it message; a stored Tag never has one.
	Link string `json:"link,omitempty"`
}

// MaxTagText is the most characters a Tag's text may have.
const MaxTagText = 30

// forbiddenSchemes are link schemes a link Resource may never open.
var forbiddenSchemes = map[string]bool{"file": true, "javascript": true, "data": true}

// problem returns what to fix in the Tag at position i (from 1), or "".
func (t Tag) problem(i int) string {
	text := strings.TrimSpace(t.Text)
	if text == "" {
		return fmt.Sprintf("tag %d has no text: give it text of 1 to %d characters", i, MaxTagText)
	}
	// Characters are Unicode code points, not what a reader sees as one
	// character: an emoji made of several code points counts as several.
	if n := utf8.RuneCountInString(text); n > MaxTagText {
		return fmt.Sprintf("tag %d %q has %d characters: shorten it to at most %d", i, text, n, MaxTagText)
	}
	if t.Link != "" {
		return fmt.Sprintf("tag %d %q has a link, but Tags are never clicked: drop link from the Tag and attach %s as a Resource of kind link", i, text, t.Link)
	}
	return ""
}

// notFullURL is the linkFault of a link with no scheme or nothing after it.
const notFullURL = "not a full URL"

// linkFault returns what keeps a link Resource's link from being opened: notFullURL, the scheme when it is a forbidden one, or "".
func linkFault(link string) string {
	u, err := url.Parse(link)
	if err != nil || !fullURL(u) {
		return notFullURL
	}
	if scheme := strings.ToLower(u.Scheme); forbiddenSchemes[scheme] {
		return scheme
	}
	return ""
}

// fullURL reports whether u has a scheme and something after it: a host for
// a web link, and a host, path or opaque part for any other.
func fullURL(u *url.URL) bool {
	switch strings.ToLower(u.Scheme) {
	case "":
		return false
	case "http", "https":
		return u.Host != ""
	}
	return u.Host != "" || u.Path != "" || u.Opaque != ""
}

// validateTags returns what to fix in the first bad Tag of ts, or "".
func validateTags(ts []Tag) string {
	for i, t := range ts {
		if msg := t.problem(i + 1); msg != "" {
			return msg
		}
	}
	return ""
}

// goodTags is ts without its bad Tags, in order. A captured Ping keeps these
// rather than being refused.
func goodTags(ts []Tag) []Tag {
	var out []Tag
	for i, t := range ts {
		if t.problem(i+1) == "" {
			out = append(out, t)
		}
	}
	return out
}

// saysFinished reports whether a Tag's text only says the work is finished,
// such as "Finished", "Finished work" or "Done".
func (t Tag) saysFinished() bool {
	words := strings.Fields(strings.ToLower(t.Text))
	if len(words) == 2 && words[1] == "work" {
		words = words[:1]
	}
	if len(words) != 1 {
		return false
	}
	switch words[0] {
	case "finished", "done", "complete", "completed":
		return true
	}
	return false
}

// withoutFinished is ts without the Tags that only say the work is finished,
// in order. A finished Ping keeps these: the Finished mark already says it.
func withoutFinished(ts []Tag) []Tag {
	var out []Tag
	for _, t := range ts {
		if !t.saysFinished() {
			out = append(out, t)
		}
	}
	return out
}
