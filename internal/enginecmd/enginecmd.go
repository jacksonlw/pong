// Package enginecmd is the Engine's command line, shared by every build of
// pong-engine.
package enginecmd

import (
	"flag"
	"fmt"
	"github.com/jacksonlw/pong/internal/devices"
	"github.com/jacksonlw/pong/wire"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"syscall"

	"github.com/jacksonlw/pong/internal/engine"
	"github.com/jacksonlw/pong/internal/harness/claudecode"
	"github.com/jacksonlw/pong/internal/harness/codex"
	"github.com/jacksonlw/pong/internal/harness/harnesses"
)

// Main runs the Engine. extend, when set, adds a build's own flags before
// they are parsed and returns what makes the carrier after; nil runs the
// Engine on Local only.
func Main(extend func() func() devices.Carrier) {
	def, err := engine.DefaultDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	dir := flag.String("dir", def, "data directory (also $PONG_HOME)")
	port := flag.Int("port", engine.DefaultPort, "preferred port; a free one is used if it is taken")
	netPort := flag.Int("devices-port", devices.DefaultNetPort, "network port for other Devices; -1 keeps this Engine off the network")
	name := flag.String("name", devices.DefaultName(), "this computer's name as other Devices show it")
	withStdin := flag.Bool("exit-with-stdin", false, "exit when standard input closes (the app holds it open, so the Engine does not outlive it)")
	var carrier func() devices.Carrier
	if extend != nil {
		carrier = extend()
	}
	flag.Parse()

	env := engine.LoginEnv()
	cfg := engine.Config{Dir: *dir, Port: *port, Harnesses: harnessesFor(*dir, env), Env: env, Runners: []engine.Runner{claudecode.Runner{}, codex.Runner{}}, Keychain: engine.SystemKeychain{}}
	// The real Engine keeps a diagnostics record of real use while its
	// folder exists: create it to turn the record on, delete it to turn it off.
	if diag := filepath.Join(*dir, engine.DiagnosticsFolder); isReal(*dir) {
		if info, err := os.Stat(diag); err == nil && info.IsDir() {
			cfg.Diagnostics = diag
		}
	}
	if *netPort >= 0 {
		cfg.Devices = devicesConfig(*dir, *name, *netPort)
		if carrier != nil {
			cfg.Devices.Carrier = carrier()
		}
	}
	e, err := engine.Start(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "pong-engine:", err)
		os.Exit(1)
	}
	fmt.Printf("Engine %s listening on %s\n", engine.Version, e.Addr.Address)
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	if *withStdin {
		go func() {
			io.Copy(io.Discard, os.Stdin)
			sig <- syscall.SIGTERM
		}()
	}
	<-sig
	e.Close()
}

// harnessesFor serves /harnesses for the human running the Engine, with the
// pong command that sits beside pong-engine (Pong.app/Contents/Helpers). env
// is the human's shell environment, where each Harness's command is found.
func harnessesFor(dir string, env []string) harnesses.ForEngine {
	home, _ := os.UserHomeDir()
	self, _ := os.Executable()
	if p, err := filepath.EvalSymlinks(self); err == nil {
		self = p
	}
	return harnesses.ForEngine{Home: home, Pong: filepath.Join(filepath.Dir(self), "pong"), EngineDir: dir, Env: env}
}

// devicesConfig puts this Engine on the network with its keys in the
// Keychain on the Mac and in a file only the user can read on Linux.
func devicesConfig(dir, name string, port int) *devices.Config {
	c := &devices.Config{Name: name, Platform: wire.PlatformLinux, NetAddr: ":" + strconv.Itoa(port), Discovery: &devices.DNSSD{Name: name}}
	if runtime.GOOS == "darwin" {
		c.Platform = wire.PlatformMac
		// Only the real data directory's keys go in the Keychain; scratch
		// Engines (PONG_HOME, -dir) keep theirs in a file inside it.
		if isReal(dir) {
			c.Keys = wire.KeychainKeyStore{Service: "Pong Device Keys", Account: "device"}
		}
	}
	return c
}

// isReal says whether dir is the human's real data directory, not a scratch
// one named by PONG_HOME or -dir.
func isReal(dir string) bool {
	def, _ := engine.DefaultDir()
	return def == dir && os.Getenv("PONG_HOME") == ""
}
