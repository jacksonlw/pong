package engine_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/jacksonlw/pong/enginetest"
	"github.com/jacksonlw/pong/internal/client"
	"github.com/jacksonlw/pong/internal/engine"
)

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// repo makes a repository named name with one commit and a subfolder.
func repo(t *testing.T, name string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(filepath.Join(dir, "src", "deep"), 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "init", "-q")
	git(t, dir, "commit", "-q", "--allow-empty", "-m", "first")
	return dir
}

func pingFrom(t *testing.T, c *client.Client, session, folder string) engine.Ping {
	t.Helper()
	p, err := c.CreatePing(client.NewPing{Finished: true, Session: session, Harness: "claude-code", Folder: folder, Question: "Carry on?"})
	if err != nil {
		t.Fatal(err)
	}
	if p.Project == "" {
		t.Fatalf("Ping from %s has no Project", folder)
	}
	return p
}

func project(t *testing.T, c *client.Client, id string) engine.Project {
	t.Helper()
	p, err := c.Project(id)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestPingInsideRepositoryBelongsToRepositoryRoot(t *testing.T) {
	c, _ := enginetest.Start(t)
	root := repo(t, "billing")
	a := pingFrom(t, c, "s1", filepath.Join(root, "src", "deep"))
	b := pingFrom(t, c, "s2", root)
	if a.Project != b.Project {
		t.Fatalf("subfolder and root are different Projects: %s, %s", a.Project, b.Project)
	}
	p := project(t, c, a.Project)
	if p.Name != "billing" || p.Colour != engine.Palette[0] {
		t.Fatalf("project %+v", p)
	}
}

func TestFolderOutsideRepositoryIsItsOwnProject(t *testing.T) {
	c, _ := enginetest.Start(t)
	base := t.TempDir()
	notes, sub := filepath.Join(base, "notes"), filepath.Join(base, "notes", "drafts")
	os.MkdirAll(sub, 0o755)
	a := pingFrom(t, c, "s1", notes)
	b := pingFrom(t, c, "s2", sub)
	if a.Project == b.Project {
		t.Fatal("two folders outside any repository are the same Project")
	}
	if n := project(t, c, b.Project).Name; n != "drafts" {
		t.Fatalf("named %q", n)
	}
}

func TestWorktreesOfOneRepositoryAreOneProject(t *testing.T) {
	c, _ := enginetest.Start(t)
	root := repo(t, "app")
	wt := filepath.Join(t.TempDir(), "app-feature")
	git(t, root, "worktree", "add", "-q", "-b", "feature", wt)
	a := pingFrom(t, c, "s1", root)
	b := pingFrom(t, c, "s2", filepath.Join(wt, "src"))
	if a.Project != b.Project {
		t.Fatalf("worktree is a different Project: %+v", project(t, c, b.Project))
	}
	if n := project(t, c, a.Project).Name; n != "app" {
		t.Fatalf("named %q", n)
	}
}

func TestNewProjectGetsLeastUsedColour(t *testing.T) {
	c, _ := enginetest.Start(t)
	base := t.TempDir()
	var ids []string
	for i, name := range []string{"a", "b", "c"} {
		dir := filepath.Join(base, name)
		os.Mkdir(dir, 0o755)
		p := project(t, c, pingFrom(t, c, name, dir).Project)
		if p.Colour != engine.Palette[i] {
			t.Fatalf("Project %d got %s, want %s", i, p.Colour, engine.Palette[i])
		}
		ids = append(ids, p.ID)
	}
	// Recolour a and b to the third colour: now the first two are unused.
	for _, id := range ids[:2] {
		if _, err := c.ChangeProject(id, engine.ChangeProject{Colour: &engine.Palette[2]}); err != nil {
			t.Fatal(err)
		}
	}
	d := filepath.Join(base, "d")
	os.Mkdir(d, 0o755)
	if got := project(t, c, pingFrom(t, c, "d", d).Project).Colour; got != engine.Palette[0] {
		t.Fatalf("fourth Project got %s, want %s", got, engine.Palette[0])
	}
}

func TestChangingProjectShowsOnItsPings(t *testing.T) {
	c, _ := enginetest.Start(t)
	ev, stop, err := c.Events(-1)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	p := pingFrom(t, c, "s1", repo(t, "web"))
	name, colour := "Website", "#123ABC"
	if _, err := c.ChangeProject(p.Project, engine.ChangeProject{Name: &name, Colour: &colour}); err != nil {
		t.Fatal(err)
	}
	png := []byte("\x89PNG\r\n\x1a\n0000IHDR")
	got, err := c.SetLogo(p.Project, png)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != name || got.Colour != colour || got.Logo == "" {
		t.Fatalf("project %+v", got)
	}
	if b, err := c.Logo(got.Logo); err != nil || string(b) != string(png) {
		t.Fatalf("logo %q, %v", b, err)
	}
	// Every Ping from the Project points at it, so the change shows on all.
	again, _ := c.Ping(p.ID)
	if again.Project != got.ID {
		t.Fatalf("Ping now in %s", again.Project)
	}
	// The interface hears the change on the event stream.
	deadline := time.After(2 * time.Second)
	for {
		select {
		case e := <-ev:
			if e.Kind == engine.EventProjectChanged && len(e.Data) > 0 {
				var pr engine.Project
				if jsonInto(e.Data, &pr) && pr.Logo == got.Logo {
					return
				}
			}
		case <-deadline:
			t.Fatal("no project.changed event with the new logo")
		}
	}
}

func TestChangeProjectRefusesBadColourAndLogo(t *testing.T) {
	c, _ := enginetest.Start(t)
	p := pingFrom(t, c, "s1", t.TempDir())
	bad := "teal"
	if _, err := c.ChangeProject(p.Project, engine.ChangeProject{Colour: &bad}); !isStatus(err, 400) {
		t.Fatalf("bad colour: %v", err)
	}
	if _, err := c.SetLogo(p.Project, []byte("not an image")); !isStatus(err, 400) {
		t.Fatalf("bad logo: %v", err)
	}
	if _, err := c.Project("nope"); !isStatus(err, 404) {
		t.Fatalf("unknown Project: %v", err)
	}
}

// A Project names the Harness of its most recently updated Session, in
// GET /projects and in project.changed.
func TestProjectNamesTheHarnessLastUsedInIt(t *testing.T) {
	c, _ := enginetest.Start(t)
	dir := repo(t, "shop")
	p := pingFrom(t, c, "s1", dir)
	if got := project(t, c, p.Project).LastHarness; got != "claude-code" {
		t.Fatalf("last_harness = %q", got)
	}
	ev, stop, err := c.Events(-1)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	if _, err := c.RegisterSession(engine.RegisterSession{Harness: "codex", ID: "t1", Folder: filepath.Join(dir, "src")}); err != nil {
		t.Fatal(err)
	}
	all, err := c.Projects()
	if err != nil || len(all) != 1 || all[0].LastHarness != "codex" {
		t.Fatalf("GET /projects = %+v, %v", all, err)
	}
	timeout := time.After(2 * time.Second)
	for {
		select {
		case e := <-ev:
			var x engine.Project
			if e.Kind == engine.EventProjectChanged && json.Unmarshal(e.Data, &x) == nil && x.LastHarness == "codex" {
				return
			}
		case <-timeout:
			t.Fatal("no project.changed names codex")
		}
	}
}
