package engine_test

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jacksonlw/pong/enginetest"
	"github.com/jacksonlw/pong/internal/client"
	"github.com/jacksonlw/pong/internal/engine"
)

func sum(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// filesIn reads the paths after "Files:" in text the Session gets.
func filesIn(t *testing.T, text string) []string {
	t.Helper()
	_, list, ok := strings.Cut(text, "Files:\n")
	if !ok {
		t.Fatalf("no Files in %q", text)
	}
	var out []string
	for _, l := range strings.Split(list, "\n") {
		out = append(out, strings.TrimPrefix(l, "- "))
	}
	return out
}

func TestAPongsFilesReachTheSessionAsPaths(t *testing.T) {
	c, dir := enginetest.Start(t)
	p := newPing(t, c)
	shot := []byte("png bytes")
	own := filepath.Join(t.TempDir(), "notes.txt")
	os.WriteFile(own, []byte("notes"), 0o644)
	got, err := c.Pong(p.ID, client.SendPong{Answer: "Staging first", Text: "see these", Files: []engine.PongFile{
		{Name: "shot.png", Size: int64(len(shot)), SHA256: sum(shot), Data: shot},
		{Name: "notes.txt", Size: 5, SHA256: sum([]byte("notes")), Path: own},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got.Pong.Text, "see these\n\nFiles:\n- ") {
		t.Fatalf("text %q", got.Pong.Text)
	}
	paths := filesIn(t, got.Pong.Text)
	if len(paths) != 2 || paths[1] != own {
		t.Fatalf("paths %q", paths)
	}
	if want := filepath.Join(dir, engine.FilesDir, "s1", "shot.png"); paths[0] != want {
		t.Fatalf("placed at %q, want %q", paths[0], want)
	}
	if b, err := os.ReadFile(paths[0]); err != nil || string(b) != "png bytes" {
		t.Fatalf("placed %q %v", b, err)
	}
	if !engine.InFilesDir(dir, paths[0]) || engine.InFilesDir(dir, own) {
		t.Fatal("only the placed File is in the Files folder")
	}
}

func TestAPongWhoseFilesCannotBePlacedIsRefusedWhole(t *testing.T) {
	c, _ := enginetest.Start(t)
	p := newPing(t, c)
	for name, f := range map[string]engine.PongFile{
		"missing":  {Name: "gone.txt", Path: "/nowhere/gone.txt"},
		"damaged":  {Name: "a.txt", SHA256: sum([]byte("other")), Data: []byte("this")},
		"no bytes": {Name: "b.txt", SHA256: sum([]byte("x"))},
		"too big":  {Name: "big.bin", Data: make([]byte, engine.MaxFilesBytes+1)},
	} {
		if _, err := c.Pong(p.ID, client.SendPong{Text: "here", Files: []engine.PongFile{f}}); !isStatus(err, 409) {
			t.Fatalf("%s: %v", name, err)
		}
	}
	if again, _ := c.Ping(p.ID); again.State != engine.StateWaiting {
		t.Fatalf("the Ping is %s; nothing should have been sent", again.State)
	}
}

func TestAStartsFilesFollowItsPrompt(t *testing.T) {
	run := &countingRunner{}
	c := enginetest.StartWith(t, engine.Config{Harnesses: harnessesOn{true, true, ""}, Runners: []engine.Runner{run}})
	data := []byte("trace")
	if _, err := c.StartSession(engine.StartSession{Harness: "fake", Folder: repo(t, "shop"), Prompt: "Fix the crash",
		Files: []engine.PongFile{{Name: "trace.log", Size: 5, SHA256: sum(data), Data: data}}}); err != nil {
		t.Fatal(err)
	}
	prompts := run.started()
	if len(prompts) != 1 || !strings.HasPrefix(prompts[0], "Fix the crash\n\nFiles:\n- ") || !strings.HasSuffix(prompts[0], "trace.log") {
		t.Fatalf("prompts %q", prompts)
	}
}
