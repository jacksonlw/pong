package generic_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jacksonlw/pong/integrations"
	"github.com/jacksonlw/pong/internal/harness/generic"
)

const pong = "/Applications/Pong.app/Contents/MacOS/pong"

func write(t *testing.T, home, rel, s string) {
	t.Helper()
	p := filepath.Join(home, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(s), 0o600); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, home, rel string) (string, bool) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(home, rel))
	return string(b), err == nil
}

func mustFind(t *testing.T, id string) generic.Harness {
	h, ok := generic.Find(id)
	if !ok {
		t.Fatalf("%s not known", id)
	}
	return h
}

// server returns the pong server entry the Harness would read.
func server(t *testing.T, cfg, key string) map[string]any {
	t.Helper()
	var v map[string]json.RawMessage
	var servers map[string]map[string]any
	if err := json.Unmarshal([]byte(cfg), &v); err != nil {
		t.Fatalf("config is not valid JSON: %v\n%s", err, cfg)
	}
	if err := json.Unmarshal(v[key], &servers); err != nil {
		t.Fatalf("servers: %v\n%s", err, cfg)
	}
	return servers["pong"]
}

func TestInstallAndRemoveRestoreExistingFilesExactly(t *testing.T) {
	cfgs := map[string]string{
		"servers present": "{\n\t\"theme\": \"dark\",\n\t\"mcpServers\": {\n\t\t\"other\": {\"command\": \"x\"}\n\t}\n}\n",
		"servers empty":   `{"mcpServers": {}, "theme":"dark"}`,
		"no servers":      "{\n  \"theme\": \"dark\"\n}",
		"empty object":    "{}",
	}
	for name, cfg := range cfgs {
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			h := mustFind(t, "gemini")
			write(t, home, h.MCPConfig, cfg)
			write(t, home, h.Instruction, "# My rules\nBe brief.\n")

			if err := h.Install(home, pong); err != nil {
				t.Fatal(err)
			}
			got, _ := read(t, home, h.MCPConfig)
			s := server(t, got, "mcpServers")
			if s["command"] != pong || s["args"].([]any)[0] != "mcp" {
				t.Fatalf("pong server = %v", s)
			}
			if strings.Contains(cfg, `"theme"`) != strings.Contains(got, `"theme"`) {
				t.Fatalf("existing setting lost:\n%s", got)
			}
			ins, _ := read(t, home, h.Instruction)
			if !strings.HasPrefix(ins, "# My rules\nBe brief.\n") || !strings.Contains(ins, integrations.StandingInstruction) {
				t.Fatalf("instruction file:\n%s", ins)
			}
			if !h.Status(home).Connected {
				t.Fatal("status not connected after install")
			}

			if err := h.Remove(home); err != nil {
				t.Fatal(err)
			}
			if got, _ := read(t, home, h.MCPConfig); got != cfg {
				t.Fatalf("config after remove:\n%q\nwant\n%q", got, cfg)
			}
			if got, _ := read(t, home, h.Instruction); got != "# My rules\nBe brief.\n" {
				t.Fatalf("instruction after remove: %q", got)
			}
			fi, _ := os.Stat(filepath.Join(home, h.MCPConfig))
			if fi.Mode().Perm() != 0o600 {
				t.Fatalf("mode changed to %v", fi.Mode())
			}
		})
	}
}

func TestInstallCreatesMissingFilesAndRemoveDeletesThem(t *testing.T) {
	home := t.TempDir()
	h := mustFind(t, "amp")
	os.MkdirAll(filepath.Join(home, ".config/amp"), 0o755)

	if err := h.Install(home, pong); err != nil {
		t.Fatal(err)
	}
	cfg, _ := read(t, home, h.MCPConfig)
	if server(t, cfg, "amp.mcpServers")["command"] != pong {
		t.Fatalf("config:\n%s", cfg)
	}
	if err := h.Remove(home); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{h.MCPConfig, h.Instruction} {
		if _, ok := read(t, home, rel); ok {
			t.Fatalf("%s left behind", rel)
		}
	}
	if err := h.Remove(home); err != nil {
		t.Fatal("second remove:", err)
	}
}

func TestInstallTwiceChangesNothing(t *testing.T) {
	home := t.TempDir()
	h := mustFind(t, "gemini")
	write(t, home, h.MCPConfig, `{"a": 1}`)
	h.Install(home, pong)
	cfg1, _ := read(t, home, h.MCPConfig)
	ins1, _ := read(t, home, h.Instruction)
	if err := h.Install(home, pong); err != nil {
		t.Fatal(err)
	}
	cfg2, _ := read(t, home, h.MCPConfig)
	ins2, _ := read(t, home, h.Instruction)
	if cfg1 != cfg2 || ins1 != ins2 {
		t.Fatal("second install changed files")
	}
}

func TestRemoveKeepsEditsMadeAfterInstall(t *testing.T) {
	home := t.TempDir()
	h := mustFind(t, "gemini")
	write(t, home, h.MCPConfig, "{\n  \"mcpServers\": {\n    \"other\": {}\n  }\n}\n")
	h.Install(home, pong)
	cfg, _ := read(t, home, h.MCPConfig)
	write(t, home, h.MCPConfig, strings.Replace(cfg, "\n}\n", ",\n  \"later\": true\n}\n", 1))
	ins, _ := read(t, home, h.Instruction)
	write(t, home, h.Instruction, ins+"Added later.\n")

	h.Remove(home)
	cfg, _ = read(t, home, h.MCPConfig)
	if want := "{\n  \"mcpServers\": {\n    \"other\": {}\n  },\n  \"later\": true\n}\n"; cfg != want {
		t.Fatalf("config:\n%s", cfg)
	}
	if ins, _ = read(t, home, h.Instruction); ins != "Added later.\n" {
		t.Fatalf("instruction: %q", ins)
	}
}

func TestFactoryGetsARaisedToolTimeLimit(t *testing.T) {
	home := t.TempDir()
	h := mustFind(t, "factory")
	os.MkdirAll(filepath.Join(home, ".factory"), 0o755)
	if err := h.Install(home, pong); err != nil {
		t.Fatal(err)
	}
	cfg, _ := read(t, home, h.MCPConfig)
	if ms, _ := server(t, cfg, "mcpServers")["timeout"].(float64); ms < 60000 {
		t.Fatalf("timeout = %v", ms)
	}
}

func TestNotInstalledHarnessIsLeftAlone(t *testing.T) {
	home := t.TempDir()
	h := mustFind(t, "gemini")
	if err := h.Install(home, pong); err == nil {
		t.Fatal("install into a missing Harness succeeded")
	}
	if entries, _ := os.ReadDir(home); len(entries) != 0 {
		t.Fatal("wrote into home")
	}
	if s := h.Status(home); s.Detected || s.Connected {
		t.Fatalf("status = %+v", s)
	}
}

func TestBrokenConfigIsNotTouched(t *testing.T) {
	home := t.TempDir()
	h := mustFind(t, "gemini")
	write(t, home, h.MCPConfig, `{"a": `)
	if err := h.Install(home, pong); err == nil {
		t.Fatal("install into broken JSON succeeded")
	}
	if got, _ := read(t, home, h.MCPConfig); got != `{"a": ` {
		t.Fatal("broken config changed")
	}
	if _, ok := read(t, home, h.Instruction); ok {
		t.Fatal("instruction written despite failure")
	}
}
