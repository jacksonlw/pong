package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTheSweepRemovesFilesOfSessionsGoneAWeek(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	old := now.Add(-filesRetention - time.Hour)
	put := func(folder string, at time.Time) string {
		p := filepath.Join(dir, FilesDir, folder, "f.txt")
		os.MkdirAll(filepath.Dir(p), 0o700)
		os.WriteFile(p, []byte("x"), 0o600)
		os.Chtimes(p, at, at)
		return filepath.Dir(p)
	}
	ended := put("ended-long-ago", old)
	live := put("still-working", old)
	recent := put("ended-yesterday", now.Add(-24*time.Hour))
	sweepFiles(dir, func(folder string) bool { return folder == "still-working" }, now)
	for d, kept := range map[string]bool{ended: false, live: true, recent: true} {
		if _, err := os.Stat(d); (err == nil) != kept {
			t.Errorf("%s kept=%v, want %v", filepath.Base(d), err == nil, kept)
		}
	}
}

func TestWithFilesPutsPathsAfterTheText(t *testing.T) {
	if got := withFiles("Fix it\n", []string{"/a", "/b"}); got != "Fix it\n\nFiles:\n- /a\n- /b" {
		t.Fatalf("%q", got)
	}
	if got := withFiles("", []string{"/a"}); got != "Files:\n- /a" {
		t.Fatalf("%q", got)
	}
	if got := withFiles("Fix it", nil); got != "Fix it" {
		t.Fatalf("%q", got)
	}
}

func TestPartsAreKeptInOrderAndCheckedWhole(t *testing.T) {
	dir := t.TempDir()
	data := []byte("hello world")
	h := sha256Hex(data)
	if err := takePart(dir, h, 6, int64(len(data)), data[6:]); err == nil {
		t.Fatal("a part out of order was kept")
	}
	if err := takePart(dir, h, 0, int64(len(data)), data[:6]); err != nil {
		t.Fatal(err)
	}
	if arrivedNow(dir, h) {
		t.Fatal("half a File counts as arrived")
	}
	if err := takePart(dir, h, 6, int64(len(data)), data[6:]); err != nil || !arrivedNow(dir, h) {
		t.Fatalf("the whole File did not arrive: %v", err)
	}
	bad := sha256Hex([]byte("other"))
	if err := takePart(dir, bad, 0, int64(len(data)), data); err == nil || arrivedNow(dir, bad) {
		t.Fatal("a damaged File was kept")
	}
	for _, c := range []struct {
		sum           string
		offset, total int64
	}{{"nothex", 0, 3}, {h, 0, MaxFilesBytes + 1}, {h, -1, 3}, {h, 2, 3}} {
		if takePart(dir, c.sum, c.offset, c.total, []byte("ab")) == nil {
			t.Errorf("%+v was kept", c)
		}
	}
}

func sha256Hex(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func arrivedNow(dir, sum string) bool {
	_, err := os.Stat(filepath.Join(dir, FilesDir, incomingDir, sum))
	return err == nil
}
