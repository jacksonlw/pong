package pongcmd

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/jacksonlw/pong/internal/devices"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/jacksonlw/pong/integrations"
	"github.com/jacksonlw/pong/internal/client"
	"github.com/jacksonlw/pong/internal/engine"
	"github.com/jacksonlw/pong/internal/harness/generic"
	"github.com/jacksonlw/pong/internal/harness/harnesses"
	"github.com/jacksonlw/pong/internal/service"
)

// startApp opens Pong.app, which starts the Engine; app is its path, or ""
// for the one macOS knows as Pong. Tests replace it.
var startApp = func(app string) error {
	if app == "" {
		return exec.Command("open", "-a", "Pong").Run()
	}
	return exec.Command("open", app).Run()
}

// serviceFor is the Engine's background service for these flags. Tests
// replace it so nothing reaches launchctl or systemctl.
var serviceFor = func(f setupFlags) service.Service {
	return service.ForCurrentUser(runtime.GOOS, *f.home, enginePath(*f.pong), *f.dir)
}

// enginePath is pong-engine beside the pong command (Pong.app/Contents/Helpers
// on the Mac, the install folder on Linux).
func enginePath(pong string) string { return filepath.Join(filepath.Dir(pong), "pong-engine") }

// setupFlags holds the flags setup, status, remove and open share.
type setupFlags struct {
	fs              *flag.FlagSet
	home, dir, pong *string
	app             *string
	wait            *time.Duration
}

func newSetupFlags(name string, stderr io.Writer) setupFlags {
	home, _ := os.UserHomeDir()
	self, _ := os.Executable()
	if p, err := filepath.EvalSymlinks(self); err == nil {
		self = p
	}
	fs, dir := flags(name, stderr)
	return setupFlags{fs: fs, dir: dir,
		home: fs.String("home", home, "home folder holding the Harnesses' configuration"),
		pong: fs.String("pong", self, "pong command the Harnesses launch"),
		app:  fs.String("app", "", "path of Pong.app (default: the one macOS knows as Pong)"),
		wait: fs.Duration("wait", 90*time.Second, "how long to wait for the Engine and for the test Pong"),
	}
}

// openApp starts Pong unless the data directory is overridden by $PONG_HOME,
// which the app does not read; then it returns what the human must do instead.
func openApp(app, dir string) (todo string, err error) {
	if os.Getenv("PONG_HOME") != "" {
		return "start the Engine for this data directory: pong-engine -dir " + dir, nil
	}
	return "", startApp(app)
}

// startEngine installs and starts the Engine's background service, then on
// the Mac opens the app for the card. Under $PONG_HOME it only says what to
// run, since the service always uses the real data directory.
func startEngine(f setupFlags) (left []string) {
	if os.Getenv("PONG_HOME") != "" {
		return []string{"start the Engine for this data directory: pong-engine -dir " + *f.dir}
	}
	todo, err := serviceFor(f).Install()
	if err != nil {
		left = append(left, fmt.Sprintf("the Engine could not be installed as a background service (%v); run pong setup again", err))
	}
	left = append(left, todo...)
	if runtime.GOOS == "darwin" {
		if err := startApp(*f.app); err != nil {
			left = append(left, fmt.Sprintf("open Pong (%v), then run pong setup again", err))
		}
	}
	return left
}

// engineUp returns a client for the running Engine, or nil.
func engineUp(dir string) *client.Client {
	c, err := client.Load(dir)
	if err != nil {
		return nil
	}
	if _, err := c.Health(); err != nil {
		return nil
	}
	return c
}

// setup installs Pong in every Harness found, starts Pong, sends a test Ping
// and waits for its Pong. It lists every step it could not do at the end and
// exits 0 only when the Pong came back. With a name it installs Pong in that
// Harness alone, or prints what to add by hand for a Harness Pong does not
// know.
func setup(args []string, stdout, stderr io.Writer) int {
	f := newSetupFlags("setup", stderr)
	pos, err := parse(f.fs, args, 1)
	if err != nil {
		return 2
	}
	if len(pos) == 1 {
		name := pos[0]
		return setupOne(name, f, stdout, stderr)
	}
	var left []string
	var found []harnesses.Entry
	for _, e := range harnesses.All(*f.dir) {
		if !e.Detect(*f.home) {
			continue
		}
		found = append(found, e)
		if err := e.Install(*f.home, *f.pong); err != nil {
			fmt.Fprintf(stdout, "%s: install failed: %v\n", e.Name(), err)
			left = append(left, fmt.Sprintf("%s: install failed (%v); run pong setup %s again", e.Name(), err, e.ID))
			continue
		}
		fmt.Fprintf(stdout, "%s: installed\n", e.Name())
	}
	if len(found) == 0 {
		fmt.Fprintln(stdout, "No supported Harness found.")
		left = append(left, "install a Harness, or add Pong to yours by hand: pong setup <name>")
	}

	deadline := time.Now().Add(*f.wait)
	c := engineUp(*f.dir)
	if c == nil {
		fmt.Fprintln(stdout, "Starting Pong.")
		if todo := startEngine(f); len(todo) > 0 {
			left = append(left, todo...)
		}
		if os.Getenv("PONG_HOME") == "" {
			for c == nil && time.Now().Before(deadline) {
				time.Sleep(50 * time.Millisecond)
				c = engineUp(*f.dir)
			}
			if c == nil {
				left = append(left, "the Engine did not answer; run pong setup again")
			}
		}
	}

	answered := false
	if c != nil && noScreen(c) {
		// A computer with no interface answers nothing itself: its Pings
		// reach a Device that has one once the two are paired.
		left = append(left, "pair this computer with a Device that shows Pong's card: run pong devices pair, check the code on that Device and approve it there, then run pong setup again")
		c = nil
	}
	if c != nil {
		fmt.Fprintln(stdout, "Sent a test Ping: answer it on the card.")
		switch p, err := testPing(c, time.Until(deadline)); {
		case err != nil:
			left = append(left, fmt.Sprintf("the test Ping could not be sent (%v); run pong setup again", err))
		case p.State == engine.StateAnswered:
			answered = true
			fmt.Fprintln(stdout, "The test Ping was answered.")
		default:
			left = append(left, "no Pong came back to the test Ping; check that Pong's card appears, then run pong setup again")
		}
	}

	for _, e := range found {
		if s := e.Status(*f.home); s.Warning != "" {
			left = append(left, e.Name()+": "+s.Warning)
		}
	}
	if len(left) > 0 {
		fmt.Fprintln(stdout, "\nLeft to do:")
		for _, l := range left {
			fmt.Fprintln(stdout, "- "+l)
		}
	}
	if !answered {
		fmt.Fprintln(stdout, "\nSetup is not finished: no Pong came back to the test Ping.")
		return 1
	}
	fmt.Fprintln(stdout, "\nSetup finished: Pong works.")
	return 0
}

// noScreen reports whether the test Ping would reach no card: on Linux, while
// this computer is in a set with no other Device. Tests replace it.
var noScreen = func(c *client.Client) bool {
	if runtime.GOOS != "linux" {
		return false
	}
	s, err := c.Devices()
	if err != nil {
		return false
	}
	for _, d := range s.Devices {
		if d.Platform == "mac" || d.Platform == "ios" { // the Devices with a card
			return false
		}
	}
	return true
}

// testPing sends the test Ping and waits up to wait for its Pong, withdrawing
// it if none comes.
func testPing(c *client.Client, wait time.Duration) (engine.Ping, error) {
	cwd, _ := os.Getwd()
	p, err := c.CreatePing(client.NewPing{Session: "pong-setup", Folder: cwd,
		Question:    "Pong is set up. Did this card reach you?",
		Answers:     []engine.Answer{{Label: "It did"}},
		BlocksAgent: true, // pong setup waits for the Pong
		Why:         "pong setup sends this test Ping and finishes when it is answered"})
	if err != nil {
		return p, err
	}
	// The test Ping's Session runs in no Harness, so nothing else would end it.
	defer c.RegisterSession(engine.RegisterSession{ID: "pong-setup", State: engine.SessionEnded})
	done := make(chan engine.Ping, 1)
	go func() {
		got, _ := c.Wait(p.ID)
		done <- got
	}()
	select {
	case got := <-done:
		return got, nil
	case <-time.After(wait):
		c.Withdraw(p.ID)
		got := <-done
		// The withdraw raced the next wait; drop any connection dialled
		// for it but never used, which would hold up the Engine's shutdown.
		c.HTTP.CloseIdleConnections()
		return got, nil
	}
}

func setupOne(name string, f setupFlags, stdout, stderr io.Writer) int {
	e, ok := harnesses.Find(*f.dir, name)
	if !ok {
		fmt.Fprintf(stdout, "Pong does not know where %s keeps its configuration. Add these two things by hand.\n\n", name)
		fmt.Fprintf(stdout, "1. An MCP server (stdio) named \"pong\":\n\n  \"pong\": %s\n\n", generic.Entry(*f.pong, 0))
		fmt.Fprintln(stdout, "   Raise its tool time limit to at least a minute if the Harness has one.")
		fmt.Fprintf(stdout, "\n2. This standing instruction, in a file the Harness always loads (often AGENTS.md):\n\n%s", integrations.StandingInstruction)
		fmt.Fprintf(stdout, "\nThe Harness %s.\n", generic.Limits)
		return 0
	}
	if err := e.Install(*f.home, *f.pong); err != nil {
		fmt.Fprintln(stderr, "pong:", err)
		return 1
	}
	fmt.Fprintf(stdout, "%s: %s\n", e.Name(), e.Status(*f.home).Note)
	return 0
}

// remove takes Pong out of every Harness, or of the one named.
func remove(args []string, stdout, stderr io.Writer) int {
	f := newSetupFlags("remove", stderr)
	pos, err := parse(f.fs, args, 1)
	if err != nil {
		return 2
	}
	es := harnesses.All(*f.dir)
	if len(pos) == 1 {
		name := pos[0]
		e, ok := harnesses.Find(*f.dir, name)
		if !ok {
			fmt.Fprintf(stderr, "pong: Pong does not know where %s keeps its configuration\n", name)
			return 1
		}
		es = []harnesses.Entry{e}
	}
	code := 0
	whole := len(pos) == 0 && os.Getenv("PONG_HOME") == ""
	if whole {
		// Out of the set first, while the Engine still runs.
		if c := engineUp(*f.dir); c != nil {
			switch told, err := c.LeaveSet(); {
			case err != nil:
				fmt.Fprintf(stdout, "Devices: %v. Revoke this computer from another Device: pong devices revoke %s\n", err, devicesName())
			case told > 0:
				fmt.Fprintln(stdout, "Devices: this computer is out of your set")
			}
		}
	}
	if whole {
		if err := serviceFor(f).Remove(); err != nil {
			fmt.Fprintf(stdout, "Engine service: could not remove it: %v\n", err)
			code = 1
		} else {
			fmt.Fprintln(stdout, "Engine service: removed")
		}
	}
	for _, e := range es {
		if !e.Detect(*f.home) {
			continue
		}
		if err := e.Remove(*f.home); err != nil {
			fmt.Fprintf(stdout, "%s: could not remove Pong: %v\n", e.Name(), err)
			code = 1
			continue
		}
		fmt.Fprintf(stdout, "%s: removed\n", e.Name())
	}
	if whole {
		if err := forgetKeys(*f.dir); err != nil {
			fmt.Fprintf(stdout, "Keys: could not remove them: %v\n", err)
			code = 1
		}
		if err := removeData(*f.dir); err != nil {
			fmt.Fprintf(stdout, "Data: could not remove it from %s: %v\n", *f.dir, err)
			code = 1
		} else {
			fmt.Fprintln(stdout, "Data and keys: removed")
		}
	}
	return code
}

// pongFiles are what the Engine keeps in its data directory. Removing Pong
// deletes these and nothing else, so a mistyped -dir cannot take anything
// with it; the directory itself goes only when that leaves it empty.
var pongFiles = []string{"pong.db", "pong.db-wal", "pong.db-shm", engine.AddressFile, "devices", "logos", "resources", "events.seq", "engine.log", "harnesses"}

func removeData(dir string) error {
	for _, name := range pongFiles {
		if err := os.RemoveAll(filepath.Join(dir, name)); err != nil {
			return err
		}
	}
	if err := os.Remove(dir); err != nil && !os.IsNotExist(err) && !isNotEmpty(err) {
		return err
	}
	return nil
}

func isNotEmpty(err error) bool {
	return strings.Contains(err.Error(), "not empty") || strings.Contains(err.Error(), "exists")
}

// forgetKeys removes this Device's keys from the Keychain, where the Engine
// keeps them for the real data directory on the Mac; elsewhere they are a
// file in the data directory. Tests replace it.
var forgetKeys = func(dir string) error {
	if def, _ := engine.DefaultDir(); runtime.GOOS != "darwin" || dir != def {
		return nil
	}
	err := exec.Command("/usr/bin/security", "delete-generic-password", "-s", "Pong Device Keys", "-a", "device").Run()
	var ee *exec.ExitError
	if errors.As(err, &ee) && ee.ExitCode() == 44 {
		return nil // none were kept
	}
	return err
}

// devicesName is this computer's name as other Devices show it.
func devicesName() string { return devices.DefaultName() }

// serviceCmd is `pong service start`: the Mac app runs it to start the
// Engine's launch agent, installing it when it is missing (opening the app
// after `pong remove` puts it back).
func serviceCmd(args []string, stdout, stderr io.Writer) int {
	f := newSetupFlags("service", stderr)
	pos, err := parse(f.fs, args, 1)
	if err != nil || len(pos) != 1 || pos[0] != "start" {
		fmt.Fprintln(stderr, "usage: pong service start")
		return 2
	}
	if os.Getenv("PONG_HOME") != "" {
		fmt.Fprintln(stderr, "pong: the Engine's service always uses the real data directory; unset PONG_HOME, or run pong-engine -dir "+*f.dir)
		return 1
	}
	s := serviceFor(f)
	if s.Installed() {
		err = s.Start()
	} else {
		var todo []string
		todo, err = s.Install()
		for _, t := range todo {
			fmt.Fprintln(stdout, "Left to do: "+t)
		}
	}
	if err != nil {
		fmt.Fprintln(stderr, "pong:", err)
		return 1
	}
	return 0
}

// openCmd opens Pong.app; if it is already running macOS brings it forward.
func openCmd(args []string, stdout, stderr io.Writer) int {
	f := newSetupFlags("open", stderr)
	if err := f.fs.Parse(args); err != nil {
		return 2
	}
	todo, err := openApp(*f.app, *f.dir)
	if err != nil {
		fmt.Fprintln(stderr, "pong: opening Pong:", err)
		return 1
	}
	if todo != "" {
		fmt.Fprintln(stdout, "Pong.app does not use $PONG_HOME; "+todo)
	}
	return 0
}

// harnessStatus prints each Harness's status after the Engine line.
func harnessStatus(home, dir string, stdout io.Writer) {
	for _, e := range harnesses.All(dir) {
		fmt.Fprintf(stdout, "%s: %s\n", e.Name(), e.Status(home).Note)
	}
}

type statusJSON struct {
	Engine    engineJSON             `json:"engine"`
	Harnesses []engine.HarnessStatus `json:"harnesses"`
}

type engineJSON struct {
	Running bool   `json:"running"`
	Version string `json:"version,omitempty"`
	Address string `json:"address,omitempty"`
}

func writeStatusJSON(e engineJSON, home, dir string, stdout io.Writer) {
	s := statusJSON{Engine: e, Harnesses: []engine.HarnessStatus{}}
	for _, h := range harnesses.All(dir) {
		s.Harnesses = append(s.Harnesses, harnesses.Report(h, home))
	}
	b, _ := json.MarshalIndent(s, "", "  ")
	fmt.Fprintf(stdout, "%s\n", b)
}
