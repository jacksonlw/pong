package claudecode

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func put(t *testing.T, dir, rel, text string) {
	t.Helper()
	p := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSkillsAreTheHumansPluginsAndTheProjects(t *testing.T) {
	home, root := t.TempDir(), t.TempDir()
	put(t, home, ".claude/skills/grilling/SKILL.md", "---\nname: grilling\n---\n")
	put(t, home, ".claude/commands/review.md", "Review.")
	plugin := filepath.Join(home, ".claude/plugins/cache/pong/pong/0.1.0")
	put(t, plugin, "skills/pong/SKILL.md", "---\nname: pong\n---\n")
	put(t, plugin, "commands/ask.md", "Ask.")
	put(t, home, ".claude/plugins/installed_plugins.json",
		`{"version":2,"plugins":{"pong@pong":[{"scope":"user","installPath":"`+plugin+`"}]}}`)
	put(t, root, ".claude/skills/deploy/SKILL.md", "---\nname: deploy\n---\n")
	put(t, root, ".claude/commands/fix.md", "Fix.")

	user, project := Adapter{}.Skills(home, root)
	if want := []string{"grilling", "pong:ask", "pong:pong", "review"}; !reflect.DeepEqual(user, want) {
		t.Fatalf("user %q, want %q", user, want)
	}
	if want := []string{"deploy", "fix"}; !reflect.DeepEqual(project, want) {
		t.Fatalf("project %q, want %q", project, want)
	}
	if _, project = (Adapter{}).Skills(home, ""); len(project) != 0 {
		t.Fatalf("no Project: %q", project)
	}
}
