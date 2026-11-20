package engine

import "testing"

func TestTheWaysOfWritingOneRemoteAreEqual(t *testing.T) {
	same := []string{
		"git@github.com:Me/App.git",
		"https://github.com/Me/App",
		"https://user@GitHub.com/Me/App.git/",
		"ssh://git@github.com:22/Me/App.git",
		"git@github.com:22/Me/App",
	}
	for _, r := range same {
		if got := normaliseRemote(r); got != "github.com/Me/App" {
			t.Errorf("%s -> %q", r, got)
		}
	}
	for _, local := range []string{"/srv/app.git", "../app", "file:///srv/app", ""} {
		if got := normaliseRemote(local); got != "" {
			t.Errorf("local %s -> %q, want no match", local, got)
		}
	}
}
