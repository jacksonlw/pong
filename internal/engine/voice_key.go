package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
)

// Keychain keeps the API key voice mode's model is reached with:
// never in the settings, never logged.
type Keychain interface {
	// Key returns the stored key, or ErrNoKey.
	Key() (string, error)
	SetKey(key string) error
	ClearKey() error
}

// ErrNoKey is returned by Keychain.Key when no key is stored.
var ErrNoKey = errors.New("no voice key is stored")

// SystemKeychain keeps the key in the login Keychain with the security
// command, as a generic password for service fly.pong.voice.
type SystemKeychain struct{}

const keychainService, keychainAccount = "fly.pong.voice", "anthropic"

func (SystemKeychain) Key() (string, error) {
	out, err := exec.Command("security", "find-generic-password", "-s", keychainService, "-a", keychainAccount, "-w").Output()
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 44 {
		return "", ErrNoKey
	}
	if err != nil {
		return "", fmt.Errorf("reading the Keychain: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// SetKey passes the key on standard input (security's interactive mode), so
// it never shows in the process list.
func (SystemKeychain) SetKey(key string) error {
	cmd := exec.Command("security", "-i")
	cmd.Stdin = strings.NewReader(fmt.Sprintf("add-generic-password -U -s %s -a %s -w \"%s\"\n", keychainService, keychainAccount, key))
	if out, err := cmd.CombinedOutput(); err != nil || len(strings.TrimSpace(string(out))) > 0 {
		return fmt.Errorf("the Keychain refused the key: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

func (SystemKeychain) ClearKey() error {
	err := exec.Command("security", "delete-generic-password", "-s", keychainService, "-a", keychainAccount).Run()
	var exit *exec.ExitError
	if err != nil && !(errors.As(err, &exit) && exit.ExitCode() == 44) {
		return fmt.Errorf("clearing the Keychain: %w", err)
	}
	return nil
}

// validKey keeps to the characters API keys use, which security's command
// line needs no quoting for.
func validKey(k string) bool {
	if k == "" || len(k) > 512 {
		return false
	}
	for _, r := range k {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.') {
			return false
		}
	}
	return true
}

// voiceKeyRoutes serves GET, PUT and DELETE /voice/key. The key is only ever
// read back by the Mac app, over this authenticated loopback API.
func voiceKeyRoutes(mux *http.ServeMux, kc Keychain) {
	unavailable := func(w http.ResponseWriter) bool {
		if kc == nil {
			writeError(w, http.StatusServiceUnavailable, "this Engine keeps no Keychain")
			return true
		}
		return false
	}
	mux.HandleFunc("GET /voice/key", func(w http.ResponseWriter, r *http.Request) {
		if unavailable(w) {
			return
		}
		k, err := kc.Key()
		if errors.Is(err, ErrNoKey) {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"key": k})
	})
	mux.HandleFunc("PUT /voice/key", func(w http.ResponseWriter, r *http.Request) {
		if unavailable(w) {
			return
		}
		var in struct {
			Key string `json:"key"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil || !validKey(in.Key) {
			writeError(w, http.StatusBadRequest, "body must be {\"key\": \"...\"} with an API key of letters, digits, '-', '_' and '.'")
			return
		}
		if err := kc.SetKey(in.Key); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"key_set": true})
	})
	mux.HandleFunc("DELETE /voice/key", func(w http.ResponseWriter, r *http.Request) {
		if unavailable(w) {
			return
		}
		if err := kc.ClearKey(); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"key_set": false})
	})
}
