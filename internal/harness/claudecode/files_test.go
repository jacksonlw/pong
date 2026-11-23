package claudecode

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestOnlyAReadOfAFilePongKeepsIsAllowed(t *testing.T) {
	dir := t.TempDir()
	read := func(tool, path string) Event {
		in, _ := json.Marshal(map[string]string{"file_path": path})
		return Event{ToolName: tool, ToolInput: in, Dir: dir}
	}
	for _, c := range []struct {
		e    Event
		want bool
	}{
		{read("Read", filepath.Join(dir, "files", "s1", "shot.png")), true},
		{read("Read", filepath.Join(dir, "files", "drafts", "x.txt")), true},
		{read("Read", filepath.Join(dir, "files")), false},
		{read("Read", filepath.Join(dir, "files", "..", "pong.db")), false},
		{read("Read", filepath.Join(dir, "pong.db")), false},
		{read("Edit", filepath.Join(dir, "files", "s1", "shot.png")), false},
		{read("Read", "/etc/hosts"), false},
		{Event{ToolName: "Read", ToolInput: json.RawMessage(`{"file_path":"x"}`)}, false},
	} {
		if got := readsFiles(c.e); got != c.want {
			t.Errorf("%s %s: %v, want %v", c.e.ToolName, c.e.ToolInput, got, c.want)
		}
	}
}
