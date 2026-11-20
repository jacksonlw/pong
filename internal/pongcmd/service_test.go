package pongcmd

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func serviceFile(home string) string {
	if runtime.GOOS == "darwin" {
		return filepath.Join(home, "Library/LaunchAgents/fly.pong.engine.plist")
	}
	return filepath.Join(home, ".config/systemd/user/pong-engine.service")
}

func TestSetupInstallsTheEngineServiceAndRemoveTakesItOut(t *testing.T) {
	home, dir := t.TempDir(), t.TempDir()
	opensEngine(t, dir)
	go answerTestPing(t, dir)
	var out bytes.Buffer
	run([]string{"setup", "-home", home, "-dir", dir, "-wait", "5s"}, &out, &out)
	b, err := os.ReadFile(serviceFile(home))
	if err != nil {
		t.Fatalf("no service installed: %v\n%s", err, out.String())
	}
	if !strings.Contains(string(b), "pong-engine") || !strings.Contains(string(b), dir) {
		t.Errorf("service does not run the Engine on %s:\n%s", dir, b)
	}
	out.Reset()
	if code := run([]string{"remove", "-home", home, "-dir", dir}, &out, &out); code != 0 {
		t.Fatalf("remove exited %d: %s", code, out.String())
	}
	if _, err := os.Stat(serviceFile(home)); !os.IsNotExist(err) {
		t.Errorf("service left behind after remove")
	}
	if !strings.Contains(out.String(), "Engine service: removed") {
		t.Errorf("remove did not say so: %s", out.String())
	}
}

func TestServiceStartInstallsAMissingService(t *testing.T) {
	home, dir := t.TempDir(), t.TempDir()
	var out bytes.Buffer
	if code := run([]string{"service", "start", "-home", home, "-dir", dir}, &out, &out); code != 0 {
		t.Fatalf("exit %d: %s", code, out.String())
	}
	if _, err := os.Stat(serviceFile(home)); err != nil {
		t.Fatal("service start did not install the missing service")
	}
}
