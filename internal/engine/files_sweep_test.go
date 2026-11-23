package engine

import (
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
