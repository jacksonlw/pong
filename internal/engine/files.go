package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// MaxFilesBytes is the most Files one Pong or first prompt may carry.
const MaxFilesBytes = 25 << 20

// FilesDir is the folder under the Engine's data folder where Files are
// kept for the Sessions that read them; the Harness may read anything in it.
const FilesDir = "files"

// filesRetention is how long a Session's Files are kept after it ends.
const filesRetention = 7 * 24 * time.Hour

// PongFile is one File the human sends with a Pong, a follow-up or a first
// prompt: a path on the computer that sent it, or its bytes.
type PongFile struct {
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256,omitempty"` // hex, of the whole File
	Path   string `json:"path,omitempty"`
	Data   []byte `json:"data,omitempty"`
}

// checkFiles refuses Files past the limit, or without a name.
func checkFiles(files []PongFile) error {
	var total int64
	for _, f := range files {
		if strings.TrimSpace(f.Name) == "" {
			return errState{"a File has no name"}
		}
		size := f.Size
		if len(f.Data) > 0 {
			size = int64(len(f.Data))
		} else if fi, err := os.Stat(f.Path); f.Path != "" && err == nil {
			size = fi.Size()
		}
		total += size
	}
	if total > MaxFilesBytes {
		return errState{fmt.Sprintf("these Files come to %d megabytes; at most %d megabytes go with one Pong", (total+(1<<20)-1)>>20, MaxFilesBytes>>20)}
	}
	return nil
}

// placeFiles puts each File where the Session's Harness can read it and
// gives their paths, in order. A path on this computer is used where it is;
// bytes are written into the Session's folder under FilesDir. Any File that
// cannot be placed refuses them all.
func placeFiles(dataDir, folder string, files []PongFile) ([]string, error) {
	if len(files) == 0 {
		return nil, nil
	}
	if err := checkFiles(files); err != nil {
		return nil, err
	}
	dir := filepath.Join(dataDir, FilesDir, safeName(folder))
	var out []string
	for _, f := range files {
		switch {
		case f.Path != "" && len(f.Data) == 0:
			fi, err := os.Stat(f.Path)
			if err != nil || fi.IsDir() {
				return nil, errState{"the File " + f.Name + " is not on the Session's computer, so nothing was sent"}
			}
			if f.SHA256 != "" {
				if sum, err := fileSum(f.Path); err != nil || sum != f.SHA256 {
					return nil, errState{"the File " + f.Name + " changed after it was added, so nothing was sent: add it again"}
				}
			}
			out = append(out, f.Path)
		case len(f.Data) > 0:
			sum := sha256.Sum256(f.Data)
			if f.SHA256 != "" && hex.EncodeToString(sum[:]) != f.SHA256 {
				return nil, errState{"the File " + f.Name + " arrived damaged, so nothing was sent"}
			}
			if err := os.MkdirAll(dir, 0o700); err != nil {
				return nil, err
			}
			p := freePath(dir, f.Name)
			if err := os.WriteFile(p, f.Data, 0o600); err != nil {
				return nil, err
			}
			out = append(out, p)
		default:
			return nil, errState{"the File " + f.Name + " did not reach the Session's computer, so nothing was sent"}
		}
	}
	return out, nil
}

// withFiles is the text the Session gets: what was written, then the Files
// by path.
func withFiles(text string, paths []string) string {
	if len(paths) == 0 {
		return text
	}
	var b strings.Builder
	b.WriteString(strings.TrimRight(text, "\n "))
	if b.Len() > 0 {
		b.WriteString("\n\n")
	}
	b.WriteString("Files:")
	for _, p := range paths {
		b.WriteString("\n- " + p)
	}
	return b.String()
}

// freePath is name in dir, numbered when a File of that name is there.
func freePath(dir, name string) string {
	name = safeName(filepath.Base(name))
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	p := filepath.Join(dir, name)
	for i := 2; ; i++ {
		if _, err := os.Stat(p); os.IsNotExist(err) {
			return p
		}
		p = filepath.Join(dir, fmt.Sprintf("%s %d%s", stem, i, ext))
	}
}

// safeName keeps a name to one folder level.
func safeName(s string) string {
	s = strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == 0 {
			return '-'
		}
		return r
	}, s)
	if s == "" || s == "." || s == ".." {
		return "file"
	}
	return s
}

func fileSum(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// InFilesDir reports whether path is inside the Engine's Files folder, so a
// Harness may read it without asking the human.
func InFilesDir(dataDir, path string) bool {
	base, err := filepath.Abs(filepath.Join(dataDir, FilesDir))
	if err != nil {
		return false
	}
	p, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(base, p)
	return err == nil && rel != "." && !strings.HasPrefix(rel, "..")
}

// sweepFiles removes the Files of Sessions that ended or went quiet more
// than filesRetention ago, and draft Files untouched as long.
func sweepFiles(dataDir string, live func(folder string) bool, now time.Time) {
	root := filepath.Join(dataDir, FilesDir)
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(root, e.Name())
		if live(e.Name()) || newestWithin(dir, now.Add(-filesRetention)) {
			continue
		}
		os.RemoveAll(dir)
	}
}

// newestWithin reports whether a File in dir changed after cutoff.
func newestWithin(dir string, cutoff time.Time) bool {
	recent := false
	filepath.WalkDir(dir, func(_ string, d os.DirEntry, err error) error {
		if err != nil || recent || d.IsDir() {
			return nil
		}
		if fi, err := d.Info(); err == nil && fi.ModTime().After(cutoff) {
			recent = true
		}
		return nil
	})
	return recent
}

// maxFilesAcross is the most Files that go with a Pong, follow-up or first
// prompt to a Session on another computer: they travel inside the message,
// which the set carries whole.
const maxFilesAcross = 128 << 10

// inlineFiles reads each File given by path on this computer, so its bytes
// go with a message to another computer.
func inlineFiles(files []PongFile) ([]PongFile, error) {
	if err := checkFiles(files); err != nil {
		return nil, err
	}
	out := make([]PongFile, len(files))
	var total int
	for i, f := range files {
		if len(f.Data) == 0 && f.Path != "" {
			b, err := os.ReadFile(f.Path)
			if err != nil {
				return nil, errState{"the File " + f.Name + " cannot be read, so nothing was sent"}
			}
			f.Data, f.Path = b, ""
		}
		total += len(f.Data)
		out[i] = f
	}
	if total > maxFilesAcross {
		return nil, errState{fmt.Sprintf("Files of more than %d kilobytes in all reach only a Session on this computer, so nothing was sent", maxFilesAcross>>10)}
	}
	return out, nil
}
