package engine_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"

	"github.com/jacksonlw/pong/enginetest"
	"github.com/jacksonlw/pong/internal/client"
	"github.com/jacksonlw/pong/internal/engine"
)

func gitRepo(t *testing.T, remote string) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{{"init", "-q"}, {"remote", "add", "origin", remote}} {
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	return dir
}

func projectNamed(t *testing.T, c *client.Client, id string) engine.Project {
	t.Helper()
	p, err := c.Project(id)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestTheSameRepositoryOnTwoComputersIsOneProject(t *testing.T) {
	book := &enginetest.Book{}
	desk := enginetest.StartComputer(t, book, "Desktop")
	laptop := enginetest.StartComputer(t, book, "Laptop")
	enginetest.Pair(t, desk, laptop)

	onDesk, err := desk.C.ProjectFor(gitRepo(t, "git@github.com:Me/App.git"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := desk.C.ChangeProject(onDesk.ID, engine.ChangeProject{Name: ptr("Storefront")}); err != nil {
		t.Fatal(err)
	}
	var onLaptop engine.Project
	enginetest.Eventually(t, "the laptop to know the desktop's Project", func() bool {
		ps, _ := laptop.C.Projects()
		return slices.ContainsFunc(ps, func(p engine.Project) bool { return p.Remote && p.Name == "Storefront" })
	})
	onLaptop, err = laptop.C.ProjectFor(gitRepo(t, "https://GitHub.com/Me/App"))
	if err != nil {
		t.Fatal(err)
	}
	if onLaptop.Key != onDesk.Key || onLaptop.Name != "Storefront" {
		t.Fatalf("not one Project: desk %+v laptop %+v", onDesk, onLaptop)
	}
	// Each keeps its own folder.
	if onLaptop.Root == onDesk.Root {
		t.Fatal("the two computers share a folder")
	}

	// A Ping on the desktop is in the laptop's Project.
	p, err := desk.C.CreatePing(client.NewPing{Finished: true, Session: "s", Folder: onDesk.Root, Question: "Ship it?", Answers: []engine.Answer{{Label: "Yes"}, {Label: "No"}}})
	if err != nil {
		t.Fatal(err)
	}
	shows(t, laptop, "the Ping in its own Project", func(p engine.Ping) bool { return p.Project == onLaptop.ID }, p.ID)

	// An edit on the laptop reaches the desktop; the last edit wins.
	if _, err := laptop.C.ChangeProject(onLaptop.ID, engine.ChangeProject{Colour: ptr("#123456")}); err != nil {
		t.Fatal(err)
	}
	enginetest.Eventually(t, "the edit to reach the desktop", func() bool {
		return projectNamed(t, desk.C, onDesk.ID).Colour == "#123456"
	})
}

func TestAPlainDirectoryIsLinkedOnce(t *testing.T) {
	book := &enginetest.Book{}
	desk := enginetest.StartComputer(t, book, "Desktop")
	laptop := enginetest.StartComputer(t, book, "Laptop")
	enginetest.Pair(t, desk, laptop)
	notes := t.TempDir()
	os.WriteFile(filepath.Join(notes, "a.md"), []byte("x"), 0o600)
	onDesk, _ := desk.C.ProjectFor(notes)
	desk.C.ChangeProject(onDesk.ID, engine.ChangeProject{Name: ptr("Research notes")})
	onLaptop, _ := laptop.C.ProjectFor(t.TempDir())
	if onLaptop.Key == onDesk.Key {
		t.Fatal("two plain directories matched by themselves")
	}
	enginetest.Eventually(t, "the laptop to know the desktop's Project", func() bool {
		_, err := laptop.C.Project(onDesk.ID)
		return err == nil
	})
	linked, err := laptop.C.LinkProject(onLaptop.ID, onDesk.ID)
	if err != nil || linked.Key != onDesk.Key || linked.Name != "Research notes" {
		t.Fatalf("link: %+v %v", linked, err)
	}
}

func TestEnablingIsPerComputerAndOffByDefault(t *testing.T) {
	book := &enginetest.Book{}
	desk := enginetest.StartComputer(t, book, "Desktop")
	laptop := enginetest.StartComputer(t, book, "Laptop")
	enginetest.Pair(t, desk, laptop)
	onDesk, _ := desk.C.ProjectFor(gitRepo(t, "git@github.com:me/api.git"))
	onLaptop, _ := laptop.C.ProjectFor(gitRepo(t, "https://github.com/me/api.git"))
	if onDesk.Enabled || onLaptop.Enabled {
		t.Fatal("a Project was enabled by default")
	}
	if _, err := laptop.C.ChangeProject(onLaptop.ID, engine.ChangeProject{Enabled: ptr(true)}); err != nil {
		t.Fatal(err)
	}
	enginetest.Eventually(t, "the desktop to see the laptop takes Sessions in it", func() bool {
		p := projectNamed(t, desk.C, onDesk.ID)
		return slices.Equal(p.EnabledOn, []string{"Laptop"}) && !p.Enabled
	})
}
func ptr[T any](v T) *T { return &v }
