package engine

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"time"
)

// Call is a tool call a Ping asks the human to allow, when a Harness's own
// permission flow needs the human's Allow to reach it later (the Claude Code
// adapter's after auto mode refused a call). When the human's Pong is the
// Ping's Allow answer, the Engine notes that exact call as allowed, once, for
// the Ping's Session. Only a Harness adapter sets it: MCP tools never do.
type Call struct {
	Tool  string          `json:"tool"`
	Input json.RawMessage `json:"input"`
	// Allow is the answer label that allows the call.
	Allow string `json:"allow"`
}

// AllowedFor is how long an Allow waits for the agent to send the call again.
const AllowedFor = 10 * time.Minute

// callKey names one tool call of one Session. A shell command's description
// is left out: the agent may write it again in other words when it retries.
func callKey(harness, session, tool string, input json.RawMessage) string {
	var in map[string]any
	json.Unmarshal(input, &in)
	if tool == "Bash" {
		delete(in, "description")
	}
	b, _ := json.Marshal(in)
	sum := sha256.Sum256([]byte(harness + "\x00" + session + "\x00" + tool + "\x00" + string(b)))
	return hex.EncodeToString(sum[:])
}

// The allows the Engine keeps. An allow is made only by the human's Pong to
// the Ping that showed the call (allowCall, from end), never by a route.
// stuck marks a call the Harness refused again after it was allowed: it is
// let stand, and not asked about again.
func newAllowed(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS allowed (
		key TEXT PRIMARY KEY,
		ping TEXT NOT NULL,
		until TEXT NOT NULL,
		stuck INTEGER NOT NULL DEFAULT 0
	)`)
	return err
}

// allowCall notes p's call as allowed when pong is its Allow answer. The
// caller holds s.mu, inside the change that answers p.
func (s *pings) allowCall(p Ping, pong *Pong) error {
	if p.Call == nil || pong == nil || p.Call.Allow == "" || pong.Answer != p.Call.Allow {
		return nil
	}
	_, err := s.db.Exec(`INSERT INTO allowed (key, ping, until) VALUES (?,?,?)
		ON CONFLICT (key) DO UPDATE SET ping = excluded.ping, until = excluded.until, stuck = 0`,
		callKey(p.Harness, p.Session, p.Call.Tool, p.Call.Input), p.ID, s.clock().Add(AllowedFor).Format(time.RFC3339Nano))
	return err
}

// unallow takes back the allow a Pong made, as when the Pong is withdrawn.
// The caller holds s.mu.
func (s *pings) unallow(pingID string) error {
	_, err := s.db.Exec(`DELETE FROM allowed WHERE ping = ? AND stuck = 0`, pingID)
	return err
}

// CallQuery names a call a Harness's hook asks about.
type CallQuery struct {
	Harness string          `json:"harness"`
	Session string          `json:"session"`
	Tool    string          `json:"tool"`
	Input   json.RawMessage `json:"input"`
	// Do is what to do: "check" whether it is allowed, "take" the allow (it
	// is used up), or "refused": the Harness refused it again; it reports
	// whether that call was allowed, now or before, so the refusal stands.
	Do string `json:"do"`
}

func (s *pings) askAllowed(q CallQuery) (bool, error) {
	key := callKey(q.Harness, q.Session, q.Tool, q.Input)
	s.mu.Lock()
	defer s.mu.Unlock()
	var until string
	var stuck bool
	err := s.db.QueryRow(`SELECT until, stuck FROM allowed WHERE key = ?`, key).Scan(&until, &stuck)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	t, _ := time.Parse(time.RFC3339Nano, until)
	live := !stuck && s.clock().Before(t)
	switch q.Do {
	case "check":
		return live, nil
	case "take":
		if !live {
			return false, nil
		}
		_, err := s.db.Exec(`DELETE FROM allowed WHERE key = ?`, key)
		return err == nil, err
	case "refused":
		if stuck {
			return true, nil
		}
		if !live {
			return false, nil
		}
		_, err := s.db.Exec(`UPDATE allowed SET stuck = 1, until = ? WHERE key = ?`, s.clock().Add(24*time.Hour).Format(time.RFC3339Nano), key)
		return err == nil, err
	}
	return false, errors.New(`do is "check", "take" or "refused"`)
}

// sweepAllowed drops allows past their time. The caller holds s.mu.
func (s *pings) sweepAllowed() error {
	_, err := s.db.Exec(`DELETE FROM allowed WHERE until < ?`, s.clock().Format(time.RFC3339Nano))
	return err
}

func (s *pings) allowedRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /allowed", func(w http.ResponseWriter, r *http.Request) {
		var q CallQuery
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&q); err != nil || q.Session == "" || q.Tool == "" {
			writeError(w, http.StatusBadRequest, "name the call: harness, session, tool and input")
			return
		}
		ok, err := s.askAllowed(q)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"allowed": ok})
	})
}
