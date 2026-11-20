package engine_test

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jacksonlw/pong/enginetest"
	"github.com/jacksonlw/pong/internal/client"
	"github.com/jacksonlw/pong/internal/engine"
)

var pngBytes = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")

func TestResourcesByFileAndTextReadBack(t *testing.T) {
	c, _ := enginetest.Start(t)
	folder := t.TempDir()
	os.WriteFile(filepath.Join(folder, "plan.md"), []byte("# Plan v1"), 0o600)
	os.WriteFile(filepath.Join(folder, "shot.png"), pngBytes, 0o600)
	p, err := c.CreatePing(client.NewPing{Finished: true, Session: "s1", Folder: folder, Question: "Approve this plan?",
		Resources: []engine.NewResource{
			{Title: "Migration plan", Kind: engine.KindDocument, File: "plan.md"},
			{Title: "Checkout page screenshot", Kind: engine.KindImage, File: filepath.Join(folder, "shot.png")},
			{Title: "Failing test output", Kind: engine.KindOutput, Content: "FAIL TestTotal"},
		}})
	if err != nil {
		t.Fatal(err)
	}
	// The Engine copied the file when the Ping was sent.
	os.WriteFile(filepath.Join(folder, "plan.md"), []byte("# Plan v2"), 0o600)

	got, err := c.Ping(p.ID)
	if err != nil || len(got.Resources) != 3 {
		t.Fatalf("ping %+v, %v", got, err)
	}
	want := []struct{ title, kind, body, ctype string }{
		{"Migration plan", "document", "# Plan v1", "text/plain"},
		{"Checkout page screenshot", "image", string(pngBytes), "image/png"},
		{"Failing test output", "output", "FAIL TestTotal", "text/plain"},
	}
	for i, w := range want {
		r := got.Resources[i]
		if r.Title != w.title || r.Kind != w.kind {
			t.Fatalf("resource %d = %+v", i, r)
		}
		b, ctype, err := c.Resource(p.ID, r.ID)
		if err != nil || string(b) != w.body || !strings.HasPrefix(ctype, w.ctype) {
			t.Fatalf("resource %d content %q type %q err %v", i, b, ctype, err)
		}
	}
	if _, _, err := c.Resource(p.ID, "nope"); !isStatus(err, http.StatusNotFound) {
		t.Fatalf("unknown resource err = %v", err)
	}
}

func TestBadResourcesAreRefused(t *testing.T) {
	c, _ := enginetest.Start(t)
	folder := t.TempDir()
	os.WriteFile(filepath.Join(folder, "locked.txt"), []byte("x"), 0o000)
	cases := []struct {
		name string
		res  engine.NewResource
		says string
	}{
		{"one word title", engine.NewResource{Title: "Plan", Kind: "text", Content: "x"}, "two to four words"},
		{"five word title", engine.NewResource{Title: "The full migration plan draft", Kind: "text", Content: "x"}, "two to four words"},
		{"unknown kind", engine.NewResource{Title: "Migration plan", Kind: "video", Content: "x"}, "kind"},
		{"neither file nor content", engine.NewResource{Title: "Migration plan", Kind: "text"}, "exactly one"},
		{"missing file", engine.NewResource{Title: "Migration plan", Kind: "file", File: "gone.md"}, "gone.md"},
		{"unreadable file", engine.NewResource{Title: "Migration plan", Kind: "file", File: filepath.Join(folder, "locked.txt")}, "locked.txt"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := c.CreatePing(client.NewPing{Finished: true, Session: "s1", Folder: folder, Question: "Approve?", Resources: []engine.NewResource{tc.res}})
			var e *client.Error
			if !errors.As(err, &e) || e.Status != http.StatusBadRequest || !strings.Contains(e.Message, tc.says) {
				t.Fatalf("err = %v, want 400 saying %q", err, tc.says)
			}
		})
	}
	if ps, _ := c.Pings(""); len(ps) != 0 {
		t.Fatalf("refused Pings were kept: %+v", ps)
	}
}

func TestAnsweredPingsRemovedAfterRetentionWaitingKept(t *testing.T) {
	c := enginetest.StartWith(t, engine.Config{Retention: 50 * time.Millisecond, SweepEvery: 10 * time.Millisecond})
	res := []engine.NewResource{{Title: "Migration plan", Kind: "text", Content: "plan"}}
	old, err := c.CreatePing(client.NewPing{Finished: true, Session: "s1", Question: "Ship it?", Resources: res})
	if err != nil {
		t.Fatal(err)
	}
	waiting, _ := c.CreatePing(client.NewPing{Finished: true, Session: "s1", Question: "And after that?", Resources: res})
	if _, err := c.Pong(old.ID, client.SendPong{Text: "yes"}); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(2 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		if _, err := c.Ping(old.ID); isStatus(err, http.StatusNotFound) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("answered Ping was not removed")
		}
	}
	if _, _, err := c.Resource(old.ID, old.Resources[0].ID); !isStatus(err, http.StatusNotFound) {
		t.Fatalf("removed Ping's Resource err = %v", err)
	}
	p, err := c.Ping(waiting.ID)
	if err != nil || p.State != engine.StateWaiting {
		t.Fatalf("waiting Ping = %+v, %v", p, err)
	}
	if b, _, err := c.Resource(waiting.ID, p.Resources[0].ID); err != nil || string(b) != "plan" {
		t.Fatalf("waiting Ping's Resource = %q, %v", b, err)
	}
}
