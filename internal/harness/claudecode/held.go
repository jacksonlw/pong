package claudecode

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/jacksonlw/pong/internal/client"
)

// A prompt Claude Code shows in the terminal is mirrored by a Ping. When the
// human answers in the terminal instead, the Ping must leave Pong at once, not
// at their next prompt. Each Ping made for a tool call is recorded
// under the Engine's data folder by the call: its id where the event carries
// one (PreToolUse), else its tool and input, which PermissionRequest carries
// unchanged into PostToolUse (checked on Claude Code 2.1.330). The call's
// PostToolUse or
// PostToolUseFailure, or the end of the turn, marks it answered in the
// Harness. A hook still holding it then sees it end with no Pong and writes
// nothing, which leaves Claude Code's own answer in place.
func init() {
	on("PostToolUse", settleCall)
	on("PostToolUseFailure", settleCall)
}

// safeName matches an id that can name a file: Claude Code's session and
// tool call ids, and nothing that could leave the held folder.
var safeName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`)

func heldDir(e Event) string {
	if e.Dir == "" || !safeName.MatchString(e.SessionID) {
		return ""
	}
	return filepath.Join(e.Dir, "held", e.SessionID)
}

// callKeys are the names a tool call is recorded under: its id, when the
// event has one, and its tool and input.
func callKeys(e Event) []string {
	var keys []string
	if safeName.MatchString(e.ToolUseID) {
		keys = append(keys, "id-"+e.ToolUseID)
	}
	if e.ToolName != "" {
		var v any
		in := []byte(e.ToolInput)
		if json.Unmarshal(e.ToolInput, &v) == nil {
			in, _ = json.Marshal(v) // the same input, however it was spaced
		}
		sum := sha256.Sum256(append([]byte(e.ToolName+"\x00"), in...))
		keys = append(keys, "call-"+hex.EncodeToString(sum[:12]))
	}
	return keys
}

// hold records that ids are the Pings made for e's tool call.
func hold(e Event, ids []string) {
	d := heldDir(e)
	keys := callKeys(e)
	if d == "" || len(keys) == 0 || len(ids) == 0 || os.MkdirAll(d, 0o700) != nil {
		return
	}
	os.WriteFile(filepath.Join(d, keys[0]), []byte(strings.Join(ids, "\n")), 0o600)
}

// release forgets e's tool call, answered or not.
func release(e Event) {
	if d := heldDir(e); d != "" {
		for _, k := range callKeys(e) {
			os.Remove(filepath.Join(d, k))
		}
	}
}

// settleCall marks the Pings of a tool call that has now run, or failed,
// answered in the Harness: Claude Code settled its prompt itself.
func settleCall(c *client.Client, e Event) (any, error) {
	if d := heldDir(e); d != "" {
		for _, k := range callKeys(e) {
			settle(c, filepath.Join(d, k))
		}
	}
	return nil, nil
}

// settleTurn marks every Ping still held for the Session's tool calls
// answered in the Harness: once its turn has ended, no call of it waits.
func settleTurn(c *client.Client, e Event) {
	d := heldDir(e)
	if d == "" {
		return
	}
	fs, _ := os.ReadDir(d)
	for _, f := range fs {
		settle(c, filepath.Join(d, f.Name()))
	}
}

func settle(c *client.Client, path string) {
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	os.Remove(path)
	for _, id := range strings.Fields(string(b)) {
		if _, err := c.AnsweredInHarness(id); err != nil {
			// Answered in Pong already: the terminal's answer came first to
			// the Session, so that Pong must not wake it again.
			c.AnsweredFirst(id)
		}
	}
}
