package engine

import (
	"slices"
	"testing"
)

// A Session Pong starts or opens again appears in a terminal window behind the
// app the human is using, so it never takes the keyboard or a click meant for
// that app: open's -g leaves the terminal app in the background.
func TestASessionsWindowOpensBehindTheAppInUse(t *testing.T) {
	if got := openArgs("/tmp/pong-open/a.command"); !slices.Equal(got, []string{"-g", "/tmp/pong-open/a.command"}) {
		t.Fatalf("open %v", got)
	}
}
