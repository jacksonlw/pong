package harness_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/jacksonlw/pong/internal/harness"
)

// write puts text in a file under dir, making its folders.
func write(t *testing.T, dir, rel, text string) {
	t.Helper()
	p := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSkillsInNamesEachSkillFolder(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "grilling/SKILL.md", "---\nname: grilling\ndescription: Grill the user.\n---\nbody")
	write(t, dir, "folder-name/SKILL.md", "---\nname: \"own-name\"\n---\n")
	write(t, dir, "hidden/SKILL.md", "---\nname: hidden\nuser-invocable: false\n---\n")
	write(t, dir, "plain/SKILL.md", "no front matter\nname: not-this\n")
	write(t, dir, "empty/notes.md", "not a Skill")
	write(t, dir, ".system/SKILL.md", "---\nname: dot\n---\n")
	got := harness.Names(harness.SkillsIn(dir, "x:"))
	if want := []string{"x:grilling", "x:own-name", "x:plain"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("%q, want %q", got, want)
	}
	if got := harness.SkillsIn(filepath.Join(dir, "missing"), ""); got != nil {
		t.Fatalf("a missing folder: %q", got)
	}
}

func TestCommandsInNamesEachMarkdownFileByItsName(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "review.md", "Review the diff.")
	write(t, dir, "frontend/component.md", "Make a component.")
	write(t, dir, "hidden.md", "---\nuser-invocable: false\n---\n")
	write(t, dir, "notes.txt", "not a command")
	write(t, dir, ".git/HEAD.md", "not a command")
	got := harness.Names(harness.CommandsIn(dir, ""))
	if want := []string{"component", "review"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("%q, want %q", got, want)
	}
}

func TestNamesSortsAndDropsRepeats(t *testing.T) {
	got := harness.Names([]string{"b", "a", ""}, []string{"a", "c"})
	if want := []string{"a", "b", "c"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("%q", got)
	}
}
