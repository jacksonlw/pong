package pongcmd

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/jacksonlw/pong/enginetest"
)

func TestPairingFromTheCommandLine(t *testing.T) {
	book := &enginetest.Book{}
	laptop := enginetest.StartComputer(t, book, "Laptop")
	box := enginetest.StartComputer(t, book, "Box")

	var pairOut bytes.Buffer
	if c := run([]string{"devices", "pair", "-dir", box.Dir}, &pairOut, &pairOut); c != 0 {
		t.Fatalf("pair exited %d: %s", c, pairOut.String())
	}

	var list bytes.Buffer
	var id string
	enginetest.Eventually(t, "Box to ask", func() bool {
		list.Reset()
		run([]string{"devices", "approve", "-dir", laptop.Dir}, &list, &list)
		m := regexp.MustCompile(`Box \(Linux\) asks to join with the code (\d{6})\. .*approve ([0-9a-f]{32})`).FindStringSubmatch(list.String())
		if m == nil {
			return false
		}
		id = m[2]
		return true
	})
	code := regexp.MustCompile(`code (\d{6})`).FindStringSubmatch(list.String())[1]

	var out bytes.Buffer
	if c := run([]string{"devices", "approve", "-dir", laptop.Dir, id}, &out, &out); c != 0 {
		t.Fatalf("approve exited %d: %s", c, out.String())
	}
	if !strings.Contains(pairOut.String(), "shows the code "+code) {
		t.Errorf("the two computers showed different codes:\n%s\n%s", pairOut.String(), list.String())
	}
	// Box joins only once its human says there that the codes match.
	if s, _ := box.C.Devices(); len(s.Devices) != 1 {
		t.Fatalf("Box joined before its human confirmed: %+v", s.Devices)
	}
	out.Reset()
	if c := run([]string{"devices", "confirm", "-dir", box.Dir}, &out, &out); c != 0 || !strings.Contains(out.String(), "now in your set") {
		t.Fatalf("confirm exited %d: %s", c, out.String())
	}
	enginetest.Eventually(t, "both to list each other", func() bool {
		out.Reset()
		run([]string{"devices", "-dir", laptop.Dir}, &out, &out)
		return strings.Contains(out.String(), "Box (Linux): reachable") && strings.Contains(out.String(), "on Local")
	})
}

func TestPairingWithNothingFound(t *testing.T) {
	alone := enginetest.StartComputer(t, &enginetest.Book{}, "Alone")
	var out bytes.Buffer
	if c := run([]string{"devices", "pair", "-dir", alone.Dir}, &out, &out); c != 1 || !strings.Contains(out.String(), "No other computer running Pong was found") {
		t.Fatalf("exit %d: %s", c, out.String())
	}
}

func TestRemovingDeletesOnlyPongsOwnFiles(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "pong.db"), []byte("x"), 0o600)
	os.WriteFile(filepath.Join(dir, "mine.txt"), []byte("keep"), 0o600)
	var out bytes.Buffer
	run([]string{"remove", "-home", t.TempDir(), "-dir", dir}, &out, &out)
	if _, err := os.Stat(filepath.Join(dir, "mine.txt")); err != nil {
		t.Fatalf("removing Pong took a file that is not Pong's: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "pong.db")); !os.IsNotExist(err) {
		t.Fatal("Pong's own file is still there")
	}
}

func TestRemovingAComputerTakesItOutOfItsSetAndLeavesNothing(t *testing.T) {
	book := &enginetest.Book{}
	desk := enginetest.StartComputer(t, book, "Desktop")
	box := enginetest.StartComputer(t, book, "Box")
	enginetest.Pair(t, desk, box)
	enginetest.Eventually(t, "the two to connect", func() bool {
		s, _ := desk.C.Devices()
		return len(s.Devices) == 2 && s.Devices[1].Reachable
	})
	var out bytes.Buffer
	run([]string{"remove", "-home", t.TempDir(), "-dir", box.Dir}, &out, &out)
	if !strings.Contains(out.String(), "out of your set") || !strings.Contains(out.String(), "Data and keys: removed") {
		t.Fatalf("remove said:\n%s", out.String())
	}
	enginetest.Eventually(t, "the desktop to drop the box", func() bool {
		s, _ := desk.C.Devices()
		return len(s.Devices) == 1
	})
	for _, name := range []string{"pong.db", "devices"} {
		if _, err := os.Stat(filepath.Join(box.Dir, name)); !os.IsNotExist(err) {
			t.Fatalf("%s is still there: %v", name, err)
		}
	}
}
