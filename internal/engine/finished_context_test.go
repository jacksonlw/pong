package engine_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/jacksonlw/pong/enginetest"
	"github.com/jacksonlw/pong/internal/client"
	"github.com/jacksonlw/pong/internal/engine"
)

// gitFolder is a repository with one committed file, changed since.
func gitFolder(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	git := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false"}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q")
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\n"), 0o600)
	git("add", ".")
	git("commit", "-qm", "first")
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("two\n"), 0o600)
	return dir
}

func changes(p engine.Ping) int {
	n := 0
	for _, r := range p.Resources {
		if r.Kind == engine.KindChange {
			n++
		}
	}
	return n
}

func TestFinishedWorkCarriesItsUncommittedDiff(t *testing.T) {
	c, _ := enginetest.Start(t)
	dir := gitFolder(t)
	p, err := c.CreatePing(client.NewPing{Session: "s1", Folder: dir, Question: "The fix is in.", Finished: true, NotVerified: "not tried in the app"})
	if err != nil {
		t.Fatal(err)
	}
	if changes(p) != 1 || p.NotVerified != "not tried in the app" {
		t.Fatalf("finished Ping %+v, want its diff and what was not checked", p)
	}

	own := []engine.NewResource{{Title: "My change", Kind: engine.KindChange, Content: "diff"}}
	p, err = c.CreatePing(client.NewPing{Session: "s1", Folder: dir, Question: "Another fix is in.", Finished: true, Resources: own})
	if err != nil || changes(p) != 1 {
		t.Fatalf("a Ping with its own change got %d, %v: Pong adds none", changes(p), err)
	}

	q, err := c.CreatePing(client.NewPing{Session: "s1", Folder: dir, Question: "Ship it?", BlocksAgent: true})
	if err != nil || changes(q) != 0 {
		t.Fatalf("a question got %d changes, %v: only finished work carries the diff", changes(q), err)
	}

	p, err = c.CreatePing(client.NewPing{Session: "s1", Folder: t.TempDir(), Question: "Done outside git.", Finished: true})
	if err != nil || changes(p) != 0 {
		t.Fatalf("outside a repository got %d changes, %v", changes(p), err)
	}
}

func TestNotVerifiedIsOnlyForFinishedWork(t *testing.T) {
	c, _ := enginetest.Start(t)
	if _, err := c.CreatePing(client.NewPing{Session: "s1", Folder: "/tmp", Question: "Ship it?", BlocksAgent: true, NotVerified: "tests"}); err == nil {
		t.Fatal("a question with not_verified was taken")
	}
}
