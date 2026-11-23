package codex

import (
	"path/filepath"

	"github.com/jacksonlw/pong/internal/harness"
)

var _ harness.SkillReader = Adapter{}

// Skills names what the human can call with $ in Codex: the Skills in their
// home folder, Codex's own system Skills and their saved prompts (as
// prompts:name), and the Project's own Skills.
func (Adapter) Skills(home, root string) (user, project []string) {
	user = harness.Names(
		harness.SkillsIn(filepath.Join(home, ".agents", "skills"), ""),
		harness.SkillsIn(filepath.Join(home, ".codex", "skills"), ""),
		harness.SkillsIn(filepath.Join(home, ".codex", "skills", ".system"), ""),
		harness.CommandsIn(filepath.Join(home, ".codex", "prompts"), "prompts:"),
	)
	if root != "" {
		project = harness.Names(
			harness.SkillsIn(filepath.Join(root, ".agents", "skills"), ""),
			harness.SkillsIn(filepath.Join(root, ".codex", "skills"), ""),
		)
	}
	return user, project
}
