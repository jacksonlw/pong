package engine_test

import (
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/jacksonlw/pong/enginetest"
	"github.com/jacksonlw/pong/internal/client"
	"github.com/jacksonlw/pong/internal/engine"
)

func TestHealthAnswersWithVersion(t *testing.T) {
	c, _ := enginetest.Start(t)
	h, err := c.Health()
	if err != nil {
		t.Fatal(err)
	}
	if h.Status != "ok" || h.Version != engine.Version {
		t.Fatalf("health = %+v", h)
	}
}

func TestAddressFileIsPrivateAndMatches(t *testing.T) {
	c, dir := enginetest.Start(t)
	path := filepath.Join(dir, engine.AddressFile)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("address file mode = %v, want 0600", info.Mode().Perm())
	}
	loaded, err := client.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Address != c.Address || loaded.Secret == "" {
		t.Fatalf("address file = %+v, engine at %s", loaded, c.Address)
	}
}

func TestSecretSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	first, err := engine.Start(engine.Config{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	first.Close()
	second, err := engine.Start(engine.Config{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if first.Addr.Secret != second.Addr.Secret {
		t.Fatal("secret changed across restart")
	}
}

func TestRefusals(t *testing.T) {
	c, _ := enginetest.Start(t)
	cases := []struct {
		name   string
		edit   func(*http.Request)
		status int
	}{
		{"no secret", func(r *http.Request) { r.Header.Del("Authorization") }, http.StatusUnauthorized},
		{"wrong secret", func(r *http.Request) { r.Header.Set("Authorization", "Bearer nope") }, http.StatusUnauthorized},
		{"foreign host", func(r *http.Request) { r.Host = "evil.example:80" }, http.StatusForbidden},
		{"foreign origin", func(r *http.Request) { r.Header.Set("Origin", "https://evil.example") }, http.StatusForbidden},
		{"local origin", func(r *http.Request) { r.Header.Set("Origin", "http://localhost:3000") }, http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, _ := c.Request("GET", "/health")
			tc.edit(req)
			resp, err := c.HTTP.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != tc.status {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tc.status)
			}
			if tc.status != http.StatusOK {
				var body struct{ Error string }
				if json.NewDecoder(resp.Body).Decode(&body); body.Error == "" {
					t.Fatal("refusal has no error message")
				}
			}
		})
	}
}

func TestFallbackWhenPortTaken(t *testing.T) {
	taken, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer taken.Close()
	port := taken.Addr().(*net.TCPAddr).Port

	dir := t.TempDir()
	e, err := engine.Start(engine.Config{Dir: dir, Port: port})
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	c, err := client.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if c.Address == taken.Addr().String() {
		t.Fatal("address file shows the taken port")
	}
	if _, err := c.Health(); err != nil {
		t.Fatalf("engine not reachable at %s: %v", c.Address, err)
	}
}
