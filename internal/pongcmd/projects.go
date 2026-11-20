package pongcmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/jacksonlw/pong/internal/client"
	"github.com/jacksonlw/pong/internal/engine"
)

// projectsCmd is `pong projects [link|enable|disable]`.
func projectsCmd(args []string, stdout, stderr io.Writer) int {
	sub := ""
	if len(args) > 0 && (args[0] == "link" || args[0] == "enable" || args[0] == "disable") {
		sub, args = args[0], args[1:]
	}
	fs, dir := flags("projects", stderr)
	folder := fs.String("folder", "", "the folder on this computer (default: the current folder)")
	pos, err := parse(fs, args, 1)
	if err != nil {
		return 2
	}
	c, err := client.Load(*dir)
	if err != nil {
		fmt.Fprintln(stderr, "pong: the Engine is not running")
		return 1
	}
	all, err := c.Projects()
	if err != nil {
		fmt.Fprintln(stderr, "pong:", err)
		return 1
	}
	here := func() (engine.Project, bool) {
		f := *folder
		if f == "" {
			f, _ = os.Getwd()
		}
		f, _ = filepath.Abs(f)
		p, err := c.ProjectFor(f)
		if err != nil {
			fmt.Fprintln(stderr, "pong:", err)
			return p, false
		}
		return p, true
	}
	switch sub {
	case "":
		for _, p := range all {
			where := p.Root
			if p.Remote {
				where = "on another computer"
			}
			on := "no computer takes Sessions from other Devices"
			if len(p.EnabledOn) > 0 {
				on = "Sessions from other Devices run on " + strings.Join(p.EnabledOn, ", ")
			}
			fmt.Fprintf(stdout, "%s (%s): %s\n", p.Name, where, on)
		}
		return 0
	case "link":
		if len(pos) != 1 {
			fmt.Fprintln(stderr, "usage: pong projects link <project name or id> [-folder DIR]")
			return 2
		}
		to, ok := findProject(all, pos[0], stderr)
		if !ok {
			return 1
		}
		p, ok := here()
		if !ok {
			return 1
		}
		linked, err := c.LinkProject(p.ID, to.ID)
		if err != nil {
			fmt.Fprintln(stderr, "pong:", err)
			return 1
		}
		fmt.Fprintf(stdout, "%s on this computer is now %s.\n", p.Root, linked.Name)
		return 0
	}
	var p engine.Project
	if len(pos) == 1 {
		var ok bool
		if p, ok = findProject(all, pos[0], stderr); !ok {
			return 1
		}
		if p.Remote {
			fmt.Fprintf(stderr, "pong: %s is not on this computer; link a folder to it first: pong projects link %q\n", p.Name, p.Name)
			return 1
		}
	} else {
		var ok bool
		if p, ok = here(); !ok {
			return 1
		}
	}
	on := sub == "enable"
	if _, err := c.ChangeProject(p.ID, engine.ChangeProject{Enabled: &on}); err != nil {
		fmt.Fprintln(stderr, "pong:", err)
		return 1
	}
	if on {
		fmt.Fprintf(stdout, "%s: this computer now takes Sessions started from your other Devices.\n", p.Name)
	} else {
		fmt.Fprintf(stdout, "%s: this computer no longer takes Sessions started from your other Devices.\n", p.Name)
	}
	return 0
}

func findProject(all []engine.Project, name string, stderr io.Writer) (engine.Project, bool) {
	var match []engine.Project
	for _, p := range all {
		if p.ID == name || strings.EqualFold(p.Name, name) {
			match = append(match, p)
		}
	}
	switch len(match) {
	case 1:
		return match[0], true
	case 0:
		fmt.Fprintf(stderr, "pong: no Project is named %s\n", name)
	default:
		fmt.Fprintf(stderr, "pong: more than one Project is named %s; use its id\n", name)
	}
	return engine.Project{}, false
}
