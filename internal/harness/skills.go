package harness

import (
	"bufio"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// SkillReader is an adapter that can name the Skills its Harness offers the
// human: those in their home folder (user) and those in a Project's folder
// (project). Only names are read, never what a Skill says.
type SkillReader interface {
	Skills(home, root string) (user, project []string)
}

// SkillsIn names the Skills in dir, one folder each holding a SKILL.md, with
// prefix before each name ("plugin:" for a plugin's). The SKILL.md's own name
// wins over its folder's; one marked user-invocable: false is left out, as
// the human cannot call it.
func SkillsIn(dir, prefix string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		file := filepath.Join(dir, e.Name(), "SKILL.md")
		if _, err := os.Stat(file); err != nil {
			continue
		}
		name, hidden := frontmatter(file)
		if hidden {
			continue
		}
		if name == "" {
			name = e.Name()
		}
		out = append(out, prefix+name)
	}
	return out
}

// CommandsIn names the commands in dir: each Markdown file, by its name
// without .md, in dir or a folder inside it, with prefix before each name.
func CommandsIn(dir, prefix string) []string {
	var out []string
	filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if path != dir && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(d.Name(), ".md") {
			if _, hidden := frontmatter(path); !hidden {
				out = append(out, prefix+strings.TrimSuffix(d.Name(), ".md"))
			}
		}
		return nil
	})
	return out
}

// Names sorts names and removes repeats.
func Names(lists ...[]string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, l := range lists {
		for _, n := range l {
			if n != "" && !seen[n] {
				seen[n] = true
				out = append(out, n)
			}
		}
	}
	sort.Strings(out)
	return out
}

// frontmatter reads a Markdown file's name and whether it says
// user-invocable: false, from its front matter only.
func frontmatter(file string) (name string, hidden bool) {
	f, err := os.Open(file)
	if err != nil {
		return "", false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	if !sc.Scan() || strings.TrimSpace(sc.Text()) != "---" {
		return "", false
	}
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "---" {
			break
		}
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		v = strings.Trim(strings.TrimSpace(v), `"'`)
		switch strings.TrimSpace(k) {
		case "name":
			name = v
		case "user-invocable":
			hidden = v == "false"
		}
	}
	return name, hidden
}
