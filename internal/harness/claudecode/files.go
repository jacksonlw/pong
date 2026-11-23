package claudecode

import (
	"encoding/json"

	"github.com/jacksonlw/pong/internal/engine"
)

// readsFiles reports whether e is a Read of a File the human sent from Pong,
// which the Engine keeps in its Files folder: the agent reads it without
// asking the human, who sent it to be read. Nothing else is allowed here.
func readsFiles(e Event) bool {
	if e.ToolName != "Read" || e.Dir == "" {
		return false
	}
	var in struct {
		FilePath string `json:"file_path"`
	}
	return json.Unmarshal(e.ToolInput, &in) == nil && in.FilePath != "" && engine.InFilesDir(e.Dir, in.FilePath)
}
