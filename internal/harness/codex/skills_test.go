package codex

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestSkillsAreTheHumansSystemPromptsAndTheProjects(t *testing.T) {
	home, root := t.TempDir(), t.TempDir()
	for rel, text := range map[string]string{
		".agents/skills/grilling/SKILL.md":             "---\nname: grilling\n---\n",
		".codex/skills/.system/skill-creator/SKILL.md": "---\nname: skill-creator\n---\n",
		".codex/prompts/standup.md":                    "Write the standup.",
	} {
		put(t, home, rel, text)
	}
	put(t, root, ".agents/skills/deploy/SKILL.md", "---\nname: deploy\n---\n")
	put(t, root, ".codex/skills/release/SKILL.md", "---\nname: release\n---\n")

	user, project := Adapter{}.Skills(home, root)
	if want := []string{"grilling", "prompts:standup", "skill-creator"}; !reflect.DeepEqual(user, want) {
		t.Fatalf("user %q, want %q", user, want)
	}
	if want := []string{"deploy", "release"}; !reflect.DeepEqual(project, want) {
		t.Fatalf("project %q, want %q", project, want)
	}
}

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
