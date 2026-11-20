package engine

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// EventSettingsChanged carries the full Settings when they change.
const EventSettingsChanged = "settings.changed"

// Corners are the places the card can sit, the first being the default.
var Corners = []string{"top-right", "top-left", "bottom-right", "bottom-left"}

// PingSounds are the Ping sound modes, the first being the default: a sound
// on a Session's first Ping only, none, or a softer one on follow-ups too.
var PingSounds = []string{"first", "off", "first-and-follow-up"}

// VoiceModels are the models voice mode can understand speech with, the
// first being the default: none yet (only local phrases), hosted (a model a
// build of the app may offer through its own account), an OpenAI-compatible endpoint, or
// Anthropic with the human's key.
var VoiceModels = []string{"none", "hosted", "endpoint", "anthropic"}

// Settings are the interface's settings, kept by the Engine so they survive
// reinstalling the app.
type Settings struct {
	CardCorner string `json:"card_corner"`
	PingSound  string `json:"ping_sound"`
	// Voice is voice mode (off by default); VoiceAutoListen starts listening
	// whenever a Ping appears, rather than from the card's microphone button.
	Voice           bool `json:"voice"`
	VoiceAutoListen bool `json:"voice_auto_listen"`
	// VoiceModel is one of VoiceModels; VoiceEndpoint and VoiceEndpointModel
	// are the endpoint's URL and model name. The Anthropic key is kept in the
	// Keychain, never here.
	VoiceModel         string `json:"voice_model"`
	VoiceEndpoint      string `json:"voice_endpoint,omitempty"`
	VoiceEndpointModel string `json:"voice_endpoint_model,omitempty"`
	// CardFollowsActiveDisplay moves the card to the display with the
	// focused window. Off, the card stays on the main display.
	CardFollowsActiveDisplay bool `json:"card_follows_active_display"`
	// FirstLaunchDone and ToldLoginDisallowed are the Mac app's first-launch
	// record: it registered itself to start at login, and it told the human
	// macOS does not allow that.
	FirstLaunchDone     bool `json:"first_launch_done"`
	ToldLoginDisallowed bool `json:"told_login_disallowed"`
}

// ChangeSettings is the body of PATCH /settings; absent fields stay.
type ChangeSettings struct {
	CardCorner               *string `json:"card_corner,omitempty"`
	PingSound                *string `json:"ping_sound,omitempty"`
	Voice                    *bool   `json:"voice,omitempty"`
	VoiceAutoListen          *bool   `json:"voice_auto_listen,omitempty"`
	VoiceModel               *string `json:"voice_model,omitempty"`
	VoiceEndpoint            *string `json:"voice_endpoint,omitempty"`
	VoiceEndpointModel       *string `json:"voice_endpoint_model,omitempty"`
	CardFollowsActiveDisplay *bool   `json:"card_follows_active_display,omitempty"`
	FirstLaunchDone          *bool   `json:"first_launch_done,omitempty"`
	ToldLoginDisallowed      *bool   `json:"told_login_disallowed,omitempty"`
}

type settings struct {
	db    *sql.DB
	pings *pings // for the event log
	mu    sync.Mutex
}

func newSettings(db *sql.DB, ps *pings) (*settings, error) {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS settings (id INTEGER PRIMARY KEY CHECK (id = 1), body TEXT NOT NULL)`)
	return &settings{db: db, pings: ps}, err
}

func (s *settings) get() (Settings, error) {
	out := Settings{CardCorner: Corners[0], PingSound: PingSounds[0], VoiceModel: VoiceModels[0]}
	var body string
	err := s.db.QueryRow(`SELECT body FROM settings WHERE id = 1`).Scan(&body)
	if errors.Is(err, sql.ErrNoRows) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	return out, json.Unmarshal([]byte(body), &out)
}

func (s *settings) change(ch ChangeSettings) (Settings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, err := s.get()
	if err != nil {
		return cur, err
	}
	if ch.CardCorner != nil {
		cur.CardCorner = *ch.CardCorner
	}
	if ch.PingSound != nil {
		cur.PingSound = *ch.PingSound
	}
	if ch.Voice != nil {
		cur.Voice = *ch.Voice
	}
	if ch.VoiceAutoListen != nil {
		cur.VoiceAutoListen = *ch.VoiceAutoListen
	}
	if ch.VoiceModel != nil {
		cur.VoiceModel = *ch.VoiceModel
	}
	if ch.VoiceEndpoint != nil {
		cur.VoiceEndpoint = *ch.VoiceEndpoint
	}
	if ch.VoiceEndpointModel != nil {
		cur.VoiceEndpointModel = *ch.VoiceEndpointModel
	}
	if ch.CardFollowsActiveDisplay != nil {
		cur.CardFollowsActiveDisplay = *ch.CardFollowsActiveDisplay
	}
	if ch.FirstLaunchDone != nil {
		cur.FirstLaunchDone = *ch.FirstLaunchDone
	}
	if ch.ToldLoginDisallowed != nil {
		cur.ToldLoginDisallowed = *ch.ToldLoginDisallowed
	}
	b, _ := json.Marshal(cur)
	if _, err := s.db.Exec(`INSERT INTO settings (id, body) VALUES (1, ?) ON CONFLICT(id) DO UPDATE SET body = excluded.body`, string(b)); err != nil {
		return cur, err
	}
	return cur, s.pings.appendEvent(EventSettingsChanged, time.Now().UTC(), withPalette(cur))
}

func (s *settings) routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /settings", func(w http.ResponseWriter, r *http.Request) {
		out, err := s.get()
		writeSettings(w, out, err)
	})
	mux.HandleFunc("PATCH /settings", func(w http.ResponseWriter, r *http.Request) {
		var in ChangeSettings
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, "body is not a change to the settings: "+err.Error())
			return
		}
		if in.CardCorner != nil && !oneOf(*in.CardCorner, Corners) {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("card_corner %q is not a corner: use one of %v", *in.CardCorner, Corners))
			return
		}
		if in.PingSound != nil && !oneOf(*in.PingSound, PingSounds) {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("ping_sound %q is not a Ping sound mode: use one of %v", *in.PingSound, PingSounds))
			return
		}
		if in.VoiceModel != nil && !oneOf(*in.VoiceModel, VoiceModels) {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("voice_model %q is not a voice model: use one of %v", *in.VoiceModel, VoiceModels))
			return
		}
		if in.VoiceEndpoint != nil && *in.VoiceEndpoint != "" && !strings.HasPrefix(*in.VoiceEndpoint, "http://") && !strings.HasPrefix(*in.VoiceEndpoint, "https://") {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("voice_endpoint %q is not an http or https URL", *in.VoiceEndpoint))
			return
		}
		out, err := s.change(in)
		writeSettings(w, out, err)
	})
}

func oneOf(c string, set []string) bool {
	for _, k := range set {
		if c == k {
			return true
		}
	}
	return false
}

// settingsView is Settings as the API shows them: with the palette a
// Project's colour is chosen from.
type settingsView struct {
	Settings
	Palette []string `json:"palette"`
}

func withPalette(s Settings) settingsView { return settingsView{s, Palette} }

func writeSettings(w http.ResponseWriter, s Settings, err error) {
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, withPalette(s))
}
