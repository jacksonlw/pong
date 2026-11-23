package claudecode

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The turn's facts come from its own entries only: its last test run and the
// documents it wrote, not those of an earlier turn.
func TestTurnFactsReadTheLastTestRunAndDocumentsOfTheTurn(t *testing.T) {
	dir := t.TempDir()
	notes := filepath.Join(dir, "notes.md")
	old := filepath.Join(dir, "old.md")
	for _, f := range []string{notes, old} {
		os.WriteFile(f, []byte("x"), 0o644)
	}
	lines := []string{
		`{"type":"user","message":{"content":"Write the old notes."}}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"w0","name":"Write","input":{"file_path":"` + old + `"}}]}}`,
		`{"type":"user","message":{"content":"Fix the test and write release notes."}}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"b1","name":"Bash","input":{"command":"go test ./..."}}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"b1","content":"FAIL\tpong"}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"b2","name":"Bash","input":{"command":"cd app && go test ./... 2>&1 | tail -3"}}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"b2","content":[{"type":"text","text":"ok  \tpong\t1.2s"}]}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"b3","name":"Bash","input":{"command":"git status"}}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"b3","content":"clean"}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"w1","name":"Write","input":{"file_path":"` + notes + `"}}]}}`,
		`{"type":"assistant","isSidechain":true,"message":{"content":[{"type":"tool_use","id":"s1","name":"Bash","input":{"command":"pytest"}}]}}`,
	}
	path := filepath.Join(dir, "t.jsonl")
	os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
	f := turnFacts(path)
	if f.TestOutput != "ok  \tpong\t1.2s" {
		t.Errorf("test output = %q", f.TestOutput)
	}
	if len(f.Documents) != 1 || f.Documents[0] != notes {
		t.Errorf("documents = %v", f.Documents)
	}
}
