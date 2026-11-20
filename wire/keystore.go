package wire

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// A KeyStore keeps one Device's keys between runs.
type KeyStore interface {
	// Load returns the stored keys, or fs.ErrNotExist when there are none.
	Load() (*Keys, error)
	Save(*Keys) error
}

// LoadOrCreate returns the Device's keys, creating and saving them once.
func LoadOrCreate(s KeyStore) (*Keys, error) {
	k, err := s.Load()
	if err == nil {
		return k, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	if k, err = NewKeys(); err != nil {
		return nil, err
	}
	if err := s.Save(k); err != nil {
		return nil, err
	}
	return k, nil
}

type storedKeys struct {
	Seal string `json:"seal"`
	Sign string `json:"sign"`
}

func encodeKeys(k *Keys) []byte {
	seal, sign := k.seeds()
	b, _ := json.Marshal(storedKeys{base64.StdEncoding.EncodeToString(seal), base64.StdEncoding.EncodeToString(sign)})
	return b
}

func decodeKeys(b []byte) (*Keys, error) {
	var s storedKeys
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, fmt.Errorf("wire: stored keys: %w", err)
	}
	seal, err1 := base64.StdEncoding.DecodeString(s.Seal)
	sign, err2 := base64.StdEncoding.DecodeString(s.Sign)
	if err := errors.Join(err1, err2); err != nil {
		return nil, fmt.Errorf("wire: stored keys: %w", err)
	}
	return KeysFromSeeds(seal, sign)
}

// FileKeyStore keeps the keys in a file only the user can read (Linux, and
// tests on any platform).
type FileKeyStore struct{ Path string }

func (s FileKeyStore) Load() (*Keys, error) {
	b, err := os.ReadFile(s.Path)
	if err != nil {
		return nil, err
	}
	return decodeKeys(b)
}

func (s FileKeyStore) Save(k *Keys) error {
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
		return err
	}
	tmp := s.Path + ".tmp"
	if err := os.WriteFile(tmp, encodeKeys(k), 0o600); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.Path)
}

// KeychainKeyStore keeps the keys as a generic password in the macOS
// login Keychain. The secret goes to `security` on standard input, never in
// its arguments, so it does not show in the process list.
type KeychainKeyStore struct {
	Service string // e.g. "Pong Device Keys"
	Account string // the Engine's data directory, so scratch Engines don't share keys
}

func (s KeychainKeyStore) Load() (*Keys, error) {
	out, err := exec.Command("/usr/bin/security", "find-generic-password", "-s", s.Service, "-a", s.Account, "-w").Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && ee.ExitCode() == 44 { // errSecItemNotFound
			return nil, fs.ErrNotExist
		}
		return nil, fmt.Errorf("wire: reading keys from the Keychain: %w", err)
	}
	return decodeKeys(bytes.TrimSpace(out))
}

func (s KeychainKeyStore) Save(k *Keys) error {
	cmd := exec.Command("/usr/bin/security", "-i")
	cmd.Stdin = strings.NewReader(fmt.Sprintf("add-generic-password -U -s %s -a %s -X %x\n",
		quote(s.Service), quote(s.Account), encodeKeys(k)))
	if out, err := cmd.CombinedOutput(); err != nil || bytes.Contains(out, []byte("error")) {
		return fmt.Errorf("wire: saving keys to the Keychain: %v %s", err, out)
	}
	return nil
}

func quote(s string) string { return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"` }
