// Package pongcmd is the pong command, shared by every build of it.
package pongcmd

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/jacksonlw/pong/internal/client"
	"github.com/jacksonlw/pong/internal/engine"
)

// A Command is one of a build's own pong subcommands.
type Command func(args []string, stdout, stderr io.Writer) int

// extra is a build's own subcommands, by name.
var extra map[string]Command

// Main runs the pong command with a build's own subcommands added, and
// exits with its status.
func Main(more map[string]Command) {
	extra = more
	os.Exit(Run(os.Args[1:], os.Stdout, os.Stderr))
}

// Run runs one pong command line and returns its exit status.
func Run(args []string, stdout, stderr io.Writer) int { return run(args, stdout, stderr) }

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		usage := "setup|status|remove|open|devices|projects|ping|inbox|pong|mcp"
		for name := range extra {
			usage += "|" + name
		}
		fmt.Fprintf(stderr, "usage: pong %s [flags]\n", usage)
		return 2
	}
	switch args[0] {
	case "status":
		return status(args[1:], stdout, stderr)
	case "ping":
		return ping(args[1:], stdout, stderr)
	case "inbox":
		return inbox(args[1:], stdout, stderr)
	case "pong":
		return sendPong(args[1:], stdout, stderr)
	case "mcp":
		return serveMCP(args[1:], os.Stdin, stdout, stderr)
	case "hook":
		return hook(args[1:], os.Stdin, stdout, stderr)
	case "statusline":
		return statusLine(args[1:], os.Stdin, stdout, stderr)
	case "setup":
		return setup(args[1:], stdout, stderr)
	case "remove":
		return remove(args[1:], stdout, stderr)
	case "projects":
		return projectsCmd(args[1:], stdout, stderr)
	case "devices":
		return devicesCmd(args[1:], stdout, stderr)
	case "service":
		return serviceCmd(args[1:], stdout, stderr)
	case "open":
		return openCmd(args[1:], stdout, stderr)
	default:
		if cmd, ok := extra[args[0]]; ok {
			return cmd(args[1:], stdout, stderr)
		}
		fmt.Fprintf(stderr, "pong: unknown command %q\n", args[0])
		return 2
	}
}

// status reports whether the Engine is running and its version, then each
// Harness's status. It exits 0 when the Engine is running and 1 when not.
func status(args []string, stdout, stderr io.Writer) int {
	def, err := engine.DefaultDir()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	home, _ := os.UserHomeDir()
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dir := fs.String("dir", def, "data directory (also $PONG_HOME)")
	homeDir := fs.String("home", home, "home folder holding the Harnesses' configuration")
	asJSON := fs.Bool("json", false, "print JSON")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	var e engineJSON
	if c, err := client.Load(*dir); err == nil {
		if h, err := c.Health(); err == nil {
			e = engineJSON{Running: true, Version: h.Version, Address: c.Address}
		}
	}
	switch {
	case *asJSON:
		writeStatusJSON(e, *homeDir, *dir, stdout)
	case e.Running:
		fmt.Fprintf(stdout, "Engine: running, version %s, at %s\n", e.Version, e.Address)
	default:
		fmt.Fprintln(stdout, "Engine: not running")
	}
	if !*asJSON {
		harnessStatus(*homeDir, *dir, stdout)
	}
	if !e.Running {
		return 1
	}
	return 0
}
