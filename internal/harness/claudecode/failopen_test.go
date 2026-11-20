package claudecode_test

import (
	"bytes"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jacksonlw/pong/internal/harness/claudecode"
)

// With no Engine listening, every hook path writes nothing at once, so Claude
// Code shows its own prompt.
func TestFailOpenWithNoEngine(t *testing.T) {
	// An address file left behind by an Engine that has stopped.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close()
	stale := t.TempDir()
	os.WriteFile(filepath.Join(stale, "engine.json"), []byte(`{"address":"`+addr+`","secret":"x"}`), 0o600)

	for _, dir := range []string{t.TempDir(), stale} {
		for _, f := range []string{"question/single.json", "question/several.json", "permission/bash.json", "permission/write.json", "permission/mcp-tool.json", "plan/exit-plan-mode.json", "stop/first.json", "prompt/typed.json"} {
			in, _ := os.ReadFile(filepath.Join("testdata", f))
			var out bytes.Buffer
			start := time.Now()
			claudecode.Hook(dir, bytes.NewReader(in), &out)
			if out.Len() != 0 {
				t.Errorf("%s: wrote %q with no Engine", f, out.String())
			}
			if d := time.Since(start); d > 200*time.Millisecond {
				t.Errorf("%s: took %v with no Engine", f, d)
			}
		}
	}
}
