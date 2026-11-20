package claudecode

import (
	"encoding/json"

	"github.com/jacksonlw/pong/internal/client"
	"github.com/jacksonlw/pong/internal/engine"
)

// ToolPrefix starts the names Claude Code gives the tools of the plugin's
// MCP server: mcp__plugin_<plugin>_<server>__<tool> (checked on 2.1.322).
const ToolPrefix = "mcp__plugin_pong_pong__"

// The plugin's MCP server (`pong mcp`) does not know Claude Code's Session
// id, so a PreToolUse hook on Pong's own tools adds it, with the folder and
// the Harness, to Pings, Notes and Thoughts the agent sends. Those Pings then belong to
// the Session that the hooks capture Pings for, and the turn's end sees them.
// The hook also allows the tools, so the human is never asked to permit Pong.
func init() {
	for _, t := range []string{engine.ToolSendPing, engine.ToolSendNote, engine.ToolSendThought} {
		on("PreToolUse/"+ToolPrefix+t, ownTool(true))
	}
	for _, t := range []string{engine.ToolWaitForPong, engine.ToolWithdrawPing} {
		on("PreToolUse/"+ToolPrefix+t, ownTool(false))
	}
}

func ownTool(sends bool) handler {
	return func(c *client.Client, e Event) (any, error) {
		d := preToolUseDecision{HookEventName: "PreToolUse", PermissionDecision: "allow"}
		if sends {
			refreshContext(c, e) // the title and figures as of this Ping (context.go)
			in := map[string]any{}
			if err := json.Unmarshal(e.ToolInput, &in); err != nil {
				return nil, err
			}
			in["session"], in["harness"] = e.SessionID, HarnessName
			if f, _ := in["folder"].(string); f == "" {
				in["folder"] = e.Cwd
			}
			d.UpdatedInput = in
		}
		return preToolUse{HookSpecificOutput: d}, nil
	}
}
