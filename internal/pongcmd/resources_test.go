package pongcmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jacksonlw/pong/enginetest"
	"github.com/jacksonlw/pong/internal/client"
)

func TestPingAttachesFileAndTextResources(t *testing.T) {
	c, dir := enginetest.Start(t)
	f := filepath.Join(t.TempDir(), "out.log")
	os.WriteFile(f, []byte("build ok"), 0o600)
	var out bytes.Buffer
	code := run([]string{"ping", "-dir", dir, "-session", "s1", "-note", "-question", "Build finished",
		"-file", "output:Build log:" + f, "-text", "text:Release notes draft:v1.2 fixes"}, &out, &out)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out.String())
	}
	ps, _ := c.Pings("")
	if len(ps) != 1 || len(ps[0].Resources) != 2 {
		t.Fatalf("pings = %+v", ps)
	}
	for i, want := range []string{"build ok", "v1.2 fixes"} {
		if b, _, err := c.Resource(ps[0].ID, ps[0].Resources[i].ID); err != nil || string(b) != want {
			t.Fatalf("resource %d = %q, %v", i, b, err)
		}
	}
}

func TestMCPSendPingAttachesResources(t *testing.T) {
	transports(t, func(t *testing.T, c *client.Client, conn mcpConn) {
		folder := t.TempDir()
		os.WriteFile(filepath.Join(folder, "fix.diff"), []byte("-a\n+b\n"), 0o600)
		_, s, isErr := call(t, conn, "send_ping", map[string]any{"folder": folder, "question": "Merge this fix?", "blocks_agent": true,
			"resources": []map[string]any{{"title": "Checkout total fix", "kind": "change", "file": "fix.diff"}}})
		if isErr {
			t.Fatalf("send = %v", s)
		}
		p, _ := c.Ping(s["ping_id"].(string))
		if len(p.Resources) != 1 {
			t.Fatalf("Ping = %+v", p)
		}
		if b, _, err := c.Resource(p.ID, p.Resources[0].ID); err != nil || string(b) != "-a\n+b\n" {
			t.Fatalf("resource = %q, %v", b, err)
		}
		text, _, isErr := call(t, conn, "send_ping", map[string]any{"folder": folder, "question": "Merge this fix?", "blocks_agent": true,
			"resources": []map[string]any{{"title": "Fix", "kind": "change", "content": "x"}}})
		if !isErr || !strings.Contains(text, "two to four words") {
			t.Fatalf("bad title = %q", text)
		}
	})
}
