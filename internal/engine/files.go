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
		case f.Path == "" && len(f.Data) == 0 && f.SHA256 != "":
			// Sent ahead in parts from another Device (onFilePart).
			in := arrived(dataDir, f.SHA256)
			if in == "" {
				return nil, errState{"the File " + f.Name + " did not reach the Session's computer, so nothing was sent"}
			}
			if err := os.MkdirAll(dir, 0o700); err != nil {
				return nil, err
			}
			p := freePath(dir, f.Name)
			if err := os.Rename(in, p); err != nil {
				return nil, err
			}
			out = append(out, p)
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

// incomingDir holds Files arriving in parts from other Devices, by SHA-256,
// until the Pong, start or follow-up naming them takes them.
const incomingDir = "incoming"

// arrivalWait is how long a Pong waits for a File whose parts it overtook.
var arrivalWait = 5 * time.Second

// arrived gives the path of a File that has arrived whole, waiting a little
// for the rest of one whose parts are still arriving; "" when it has not.
func arrived(dataDir, sum string) string {
	if !validSum(sum) {
		return ""
	}
	p := filepath.Join(dataDir, FilesDir, incomingDir, sum)
	for end := time.Now().Add(arrivalWait); ; time.Sleep(100 * time.Millisecond) {
		if _, err := os.Stat(p); err == nil {
			return p
		}
		// Waits only for a File whose parts have begun to arrive.
		if _, err := os.Stat(p + ".part"); err != nil || time.Now().After(end) {
			return ""
		}
	}
}

func validSum(s string) bool {
	if len(s) != 64 {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

// takePart writes one part of a File arriving from another Device. Parts
// come in order; a part at offset 0 starts the File again. Once all of it is
// in and its SHA-256 matches, it is kept under its sum for placeFiles.
func takePart(dataDir, sum string, offset, total int64, data []byte) error {
	if !validSum(sum) || total <= 0 || total > MaxFilesBytes || offset < 0 || offset+int64(len(data)) > total {
		return errState{"a File part out of bounds"}
	}
	dir := filepath.Join(dataDir, FilesDir, incomingDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	whole := filepath.Join(dir, sum)
	if _, err := os.Stat(whole); err == nil {
		return nil // already here, from an earlier send
	}
	part := whole + ".part"
	flags := os.O_WRONLY | os.O_CREATE
	if offset == 0 {
		flags |= os.O_TRUNC
	}
	f, err := os.OpenFile(part, flags, 0o600)
	if err != nil {
		return err
	}
	fi, _ := f.Stat()
	if fi != nil && fi.Size() != offset {
		f.Close()
		return errState{"a File part out of order"}
	}
	_, err = f.WriteAt(data, offset)
	f.Close()
	if err != nil || offset+int64(len(data)) < total {
		return err
	}
	if got, err := fileSum(part); err != nil || got != sum {
		os.Remove(part)
		return errState{"a File arrived damaged"}
	}
	return os.Rename(part, whole)
}

// sendable reads each File given by path on this computer and gives it as it
// goes to another computer: by name, size and sum, its bytes sent ahead in
// parts by send.
func sendable(files []PongFile, send func(sum string, offset, total int64, data []byte) error) ([]PongFile, error) {
	if err := checkFiles(files); err != nil {
		return nil, err
	}
	out := make([]PongFile, len(files))
	for i, f := range files {
		data := f.Data
		if len(data) == 0 && f.Path != "" {
			b, err := os.ReadFile(f.Path)
			if err != nil {
				return nil, errState{"the File " + f.Name + " cannot be read, so nothing was sent"}
			}
			data = b
		}
		if len(data) == 0 {
			out[i] = f // sent ahead already, by its sum
			continue
		}
		h := sha256.Sum256(data)
		sum := hex.EncodeToString(h[:])
		for off := 0; off < len(data); off += partSize {
			end := min(off+partSize, len(data))
			if err := send(sum, int64(off), int64(len(data)), data[off:end]); err != nil {
				return nil, err
			}
		}
		out[i] = PongFile{Name: f.Name, Size: int64(len(data)), SHA256: sum}
	}
	return out, nil
}
