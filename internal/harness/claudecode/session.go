package claudecode

import (
	"encoding/json"

	"github.com/jacksonlw/pong/integrations"
	"github.com/jacksonlw/pong/internal/client"
	"github.com/jacksonlw/pong/internal/engine"
	"github.com/jacksonlw/pong/internal/harness/attended"
)

func init() {
	on("SessionStart", sessionStart)
	on("SessionEnd", sessionEnd)
}

// sessionStart registers the Session with the Engine and gives the agent the
// standing instruction as additional context. A Session Pong opened in a
// terminal is already registered under this id, and keeps what was recorded.
//
// A Session opened again (source "resume") is idle until a prompt reaches it:
// one moved into the Claude app sits there, and its listener waits (move.go).
func sessionStart(c *client.Client, e Event) (any, error) {
	var s struct {
		Source string `json:"source"`
	}
	json.Unmarshal(e.Raw, &s)
	state := engine.SessionWorking
	if s.Source == "resume" {
		state = engine.SessionIdle
	}
	app, name := desktopApp() // context.go
	if _, err := c.RegisterSession(engine.RegisterSession{
		Harness: HarnessName, ID: e.SessionID, Folder: e.Cwd, State: state,
		ResumeCommand: Runner{}.ResumeCommand(e.Cwd, e.SessionID),
		AppCommand:    app, AppName: name,
		PID:         attended.HarnessPID(), // the Engine ends the Session if it dies unseen
		Transcript:  e.TranscriptPath,      // and makes it idle if a turn ends unseen
		FirstPrompt: readFirstPrompt(e.TranscriptPath),
	}); err != nil {
		return nil, err
	}
	return sessionStartOutput{HookSpecificOutput: additionalContext{
		HookEventName:     "SessionStart",
		AdditionalContext: integrations.StandingInstruction + RetryInstruction + CommandInstruction,
	}}, nil
}

// sessionEnd records the Session as ended. Claude Code reads nothing back.
//
// A copy that no longer holds the Session does not end it. A Session moved
// into the Claude app (move.go) is open there before its first copy ends:
// seen on Claude Code 2.1.326, the app's copy registered its process 0.13
// seconds before the first copy's SessionEnd arrived.
func sessionEnd(c *client.Client, e Event) (any, error) {
	if s, err := c.Session(HarnessName, e.SessionID); err == nil {
		if me := attended.HarnessPID(); me > 0 && s.PID > 0 && s.PID != me {
			return nil, nil
		}
	}
	_, err := c.RegisterSession(engine.RegisterSession{Harness: HarnessName, ID: e.SessionID, State: engine.SessionEnded})
	return nil, err
}

type sessionStartOutput struct {
	HookSpecificOutput additionalContext `json:"hookSpecificOutput"`
}

type additionalContext struct {
	HookEventName     string `json:"hookEventName"`
	AdditionalContext string `json:"additionalContext"`
}
