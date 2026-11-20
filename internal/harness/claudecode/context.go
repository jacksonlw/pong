package claudecode

import (
	"bufio"
	"encoding/json"
	"os"
	"regexp"

	"github.com/jacksonlw/pong/internal/client"
	"github.com/jacksonlw/pong/internal/engine"
)

// refreshContext reads the Session's context-window figures from the
// transcript Claude Code names in e and records them on the Session, so a Ping
// sent next shows the state at the moment of asking. It is best effort: a
// missing transcript, or a format it does not recognise, records nothing.
//
// Claude Code 2.1.322 states, in each assistant entry of the transcript, the
// model and the token counts of that API response; the context in use is its
// input, cache-creation and cache-read tokens. It states neither the window
// size nor the cost there at the moment of asking (the window and the used
// percentage are only in the status line input; the transcript's cost-state
// entry is written when the process ends), so Window, UsedFraction and CostUSD
// stay absent.
//
// The same reading records the Session's title as Claude Code shows it, and
// the command that opens the Session in the Claude desktop app when it runs
// there, so the card names the Session as the human knows it and a click
// reaches it.
func refreshContext(c *client.Client, e Event) {
	ctx, title := readTranscript(e.TranscriptPath)
	app, name := desktopApp()
	if ctx == nil && title == "" && app == "" {
		return
	}
	c.RegisterSession(engine.RegisterSession{Harness: HarnessName, ID: e.SessionID, Folder: e.Cwd, Context: ctx, Name: title, AppCommand: app, AppName: name})
}

// hostSession matches the Claude desktop app's own id for a Session, the only
// form its link takes.
var hostSession = regexp.MustCompile(`^local_[A-Za-z0-9-]{1,64}$`)

// desktopAppCommand is the command that brings this hook's Session forward in
// the Claude desktop app, or "" when the Session does not run there. The app
// (6.6395) gives each Session's processes its own id for the Session in
// CLAUDE_CODE_HOST_SESSION_ID and opens that Session on this link.
func desktopAppCommand() string {
	id := os.Getenv("CLAUDE_CODE_HOST_SESSION_ID")
	if !hostSession.MatchString(id) {
		return ""
	}
	return "open 'claude://code/continue?session=" + id + "'"
}

// transcriptEntry is the parts of one transcript line read here. Claude Code
// 2.1.322 writes the title it shows for the Session as custom-title entries
// (the desktop app's title, or the human's own name for it) and, in a
// terminal, as ai-title entries; the last one is current.
type transcriptEntry struct {
	Type        string `json:"type"`
	IsSidechain bool   `json:"isSidechain"`
	CustomTitle string `json:"customTitle"`
	AITitle     string `json:"aiTitle"`
	Message     struct {
		Model string `json:"model"`
		Usage *struct {
			InputTokens              *int64 `json:"input_tokens"`
			CacheCreationInputTokens *int64 `json:"cache_creation_input_tokens"`
			CacheReadInputTokens     *int64 `json:"cache_read_input_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

// readTranscript returns the figures of the last main-thread assistant entry
// in the transcript at path, nil when there is none it recognises, and the
// Session's title, "" when the transcript states none. A name the human or the
// desktop app gave wins over one Claude Code made up in a terminal.
func readTranscript(path string) (*engine.Context, string) {
	if path == "" {
		return nil, ""
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, ""
	}
	defer f.Close()
	var last *engine.Context
	var custom, ai string
	r := bufio.NewReader(f)
	for {
		line, err := r.ReadBytes('\n')
		var en transcriptEntry
		if json.Unmarshal(line, &en) == nil {
			switch {
			case en.Type == "custom-title" && en.CustomTitle != "":
				custom = en.CustomTitle
			case en.Type == "ai-title" && en.AITitle != "":
				ai = en.AITitle
			case en.Type == "assistant" && !en.IsSidechain &&
				en.Message.Model != "" && en.Message.Model != "<synthetic>" && en.Message.Usage != nil:
				u := en.Message.Usage
				if u.InputTokens != nil && u.CacheCreationInputTokens != nil && u.CacheReadInputTokens != nil {
					n := *u.InputTokens + *u.CacheCreationInputTokens + *u.CacheReadInputTokens
					last = &engine.Context{Tokens: &n, Model: en.Message.Model}
				}
			}
		}
		if err != nil {
			if custom == "" {
				custom = ai
			}
			return last, custom
		}
	}
}

// desktopApp is desktopAppCommand with the app's name, both ""
// when the Session does not run there.
func desktopApp() (command, name string) {
	if c := desktopAppCommand(); c != "" {
		return c, DesktopAppName
	}
	return "", ""
}
