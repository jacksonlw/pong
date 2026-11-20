package engine

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// diagnose appends one event to the diagnostics journal, one JSON line per
// event in a file per day, when the Engine keeps one. The journal is never
// swept, so it keeps the human's real Pings and Pongs for study after
// retention removes them. A failure to write it never stops the Engine
//. The caller holds s.mu.
func (s *pings) diagnose(seq int64, kind string, at time.Time, data []byte) {
	if s.diag == "" {
		return
	}
	line, err := json.Marshal(Event{Seq: seq, Kind: kind, At: at, Data: data})
	if err != nil {
		return
	}
	if err := os.MkdirAll(s.diag, 0o700); err != nil {
		return
	}
	f, err := os.OpenFile(filepath.Join(s.diag, at.UTC().Format("2006-01-02")+".jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	f.Write(append(line, '\n'))
	f.Close()
}
