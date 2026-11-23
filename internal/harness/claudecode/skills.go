package claudecode

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/jacksonlw/pong/internal/harness"
)

var _ harness.SkillReader = Adapter{}

// Skills names what the human can call with / in Claude Code: their own
// Skills and commands, each installed plugin's as plugin:name, and the
// Project's own. Claude Code's built-in commands are not Skills.
func (Adapter) Skills(home, root string) (user, project []string) {
	dot := filepath.Join(home, ".claude")
	user = harness.Names(
		harness.SkillsIn(filepath.Join(dot, "skills"), ""),
		harness.CommandsIn(filepath.Join(dot, "commands"), ""),
		pluginSkills(dot),
	)
	if root != "" {
		project = harness.Names(
			harness.SkillsIn(filepath.Join(root, ".claude", "skills"), ""),
			harness.CommandsIn(filepath.Join(root, ".claude", "commands"), ""),
		)
	}
	return user, project
}

// pluginSkills names the Skills and commands of every plugin Claude Code has
// installed, from its record of them.
func pluginSkills(dot string) []string {
	b, err := os.ReadFile(filepath.Join(dot, "plugins", "installed_plugins.json"))
	if err != nil {
		return nil
	}
	var rec struct {
		Plugins map[string][]struct {
			InstallPath string `json:"installPath"`
		} `json:"plugins"`
	}
	if json.Unmarshal(b, &rec) != nil {
		return nil
	}
	var out []string
	for id, installs := range rec.Plugins {
		name, _, _ := strings.Cut(id, "@")
		for _, in := range installs {
			if in.InstallPath == "" {
				continue
			}
			out = append(out, harness.SkillsIn(filepath.Join(in.InstallPath, "skills"), name+":")...)
			out = append(out, harness.CommandsIn(filepath.Join(in.InstallPath, "commands"), name+":")...)
		}
	}
	return out
}
