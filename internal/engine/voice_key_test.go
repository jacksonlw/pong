package engine_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/jacksonlw/pong/enginetest"
	"github.com/jacksonlw/pong/internal/client"
	"github.com/jacksonlw/pong/internal/engine"
)

// fakeKeychain stands in for the macOS Keychain, so tests never touch the real one.
type fakeKeychain struct {
	mu  sync.Mutex
	key string
}

func (k *fakeKeychain) Key() (string, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.key == "" {
		return "", engine.ErrNoKey
	}
	return k.key, nil
}
func (k *fakeKeychain) SetKey(s string) error { k.mu.Lock(); k.key = s; k.mu.Unlock(); return nil }
func (k *fakeKeychain) ClearKey() error       { k.mu.Lock(); k.key = ""; k.mu.Unlock(); return nil }

func TestVoiceKeyIsKeptInTheKeychainNeverInTheSettings(t *testing.T) {
	kc := &fakeKeychain{}
	c, dir := enginetest.StartWithDir(t, engine.Config{Keychain: kc})
	var e *client.Error
	if _, err := c.VoiceKey(); !errors.As(err, &e) || e.Status != 404 {
		t.Fatalf("no key yet: want 404, got %v", err)
	}
	const key = "sk-ant-api03-test_KEY.1"
	if err := c.SetVoiceKey(key); err != nil {
		t.Fatal(err)
	}
	if kc.key != key {
		t.Fatalf("Keychain holds %q", kc.key)
	}
	if got, err := c.VoiceKey(); err != nil || got != key {
		t.Fatalf("read back %q, %v", got, err)
	}
	vm := "anthropic"
	if _, err := c.ChangeSettings(engine.ChangeSettings{VoiceModel: &vm}); err != nil {
		t.Fatal(err)
	}
	// The key is in no file the Engine writes.
	filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			if b, _ := os.ReadFile(p); strings.Contains(string(b), key) {
				t.Errorf("key written to %s", p)
			}
		}
		return nil
	})
	if err := c.ClearVoiceKey(); err != nil || kc.key != "" {
		t.Fatalf("clear: %v, Keychain holds %q", err, kc.key)
	}
	if err := c.SetVoiceKey("a\" -T /bin/sh"); !errors.As(err, &e) || e.Status != 400 {
		t.Fatalf("a key with quotes: want 400, got %v", err)
	}
}

func TestEngineWithoutAKeychainKeepsNoKey(t *testing.T) {
	c, _ := enginetest.Start(t)
	var e *client.Error
	if err := c.SetVoiceKey("abc"); !errors.As(err, &e) || e.Status != 503 {
		t.Fatalf("want 503, got %v", err)
	}
}
