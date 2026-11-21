package engine

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/jacksonlw/pong/internal/tagicons"
)

// Palette is the set of colours a new Project is given from. A new Project
// gets the one the fewest Projects use, the earlier one on a tie.
var Palette = []string{"#E5484D", "#F76B15", "#FFC53D", "#30A46C", "#12A594", "#0090FF", "#6E56CF", "#D6409F"}

// EventProjectChanged carries the full Project when one is created or changed.
const EventProjectChanged = "project.changed"

// Project is a folder, taken as its repository root, that Pong recognised
// from a Ping.
type Project struct {
	ID     string `json:"id"`
	Root   string `json:"root"`
	Name   string `json:"name"`
	Colour string `json:"colour"`
	// Logo is the path to fetch the logo from, changing whenever it does;
	// absent when the Project has none.
	Logo string `json:"logo,omitempty"`
	// LastHarness is the Harness of the Project's most recently updated
	// Session; absent when it has none.
	LastHarness string    `json:"last_harness,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`

	// Across computers. Key is the same on every computer the
	// Project is on: its repository's remote, normalised, or the key of the
	// Project it was linked to. EditedAt is when its name, colour or logo last
	// changed on any Device: the last edit wins.
	Key       string    `json:"key,omitempty"`
	GitRemote string    `json:"git_remote,omitempty"`
	EditedAt  time.Time `json:"edited_at"`
	// Enabled is whether this computer takes Sessions started from other
	// Devices in this Project. Off by default; kept per computer.
	Enabled bool `json:"enabled"`

	// Shown by the local API only, never stored: the computers that take
	// Sessions in it, and, on a Project only another computer has, which.
	EnabledOn []string  `json:"enabled_on,omitempty"`
	Remote    bool      `json:"remote,omitempty"`
	Computer  *Computer `json:"computer,omitempty"`
}

// projects stores Projects and Sessions.
type projects struct {
	takesText  func(Session) bool // set by the starter; may be nil
	stopsTurns func(Session) bool // set by the starter; may be nil
	// turnOver reads a Session's transcript (TurnReader); set by the starter, may be nil.
	turnOver func(Session) (ended bool, written time.Time, ok bool)
	dir        string             // data directory, set by the starter; where a stop is marked
	db         *sql.DB
	pings      *pings     // for the event log
	logos      string     // directory holding logo files, one per Project id
	mu         sync.Mutex // held while recognising or changing a Project
	smu        sync.Mutex // held while registering or changing a Session
}

func newProjects(db *sql.DB, ps *pings, dir string) (*projects, error) {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS projects (
		id TEXT PRIMARY KEY,
		root TEXT NOT NULL UNIQUE,
		body TEXT NOT NULL
	);
	CREATE TABLE IF NOT EXISTS sessions (
		harness TEXT NOT NULL,
		id TEXT NOT NULL,
		body TEXT NOT NULL,
		PRIMARY KEY (harness, id)
	);
	CREATE TABLE IF NOT EXISTS tag_colours (
		project TEXT NOT NULL,
		text TEXT NOT NULL,
		colour TEXT NOT NULL,
		PRIMARY KEY (project, text)
	)`)
	if err != nil {
		return nil, err
	}
	pr := &projects{db: db, pings: ps, logos: filepath.Join(dir, "logos")}
	ps.place = pr.place
	return pr, pr.keyAll()
}

// repositoryRoot returns the root of the repository holding folder, the main
// repository's root for a worktree, or folder itself outside any repository.
func repositoryRoot(folder string) string {
	folder = filepath.Clean(folder)
	if real, err := filepath.EvalSymlinks(folder); err == nil {
		folder = real
	}
	for dir := folder; ; dir = filepath.Dir(dir) {
		gitPath := filepath.Join(dir, ".git")
		fi, err := os.Stat(gitPath)
		if err == nil {
			if fi.IsDir() {
				return dir
			}
			return worktreeMain(dir, gitPath)
		}
		if parent := filepath.Dir(dir); parent == dir {
			return folder
		}
	}
}

// worktreeMain follows a .git file to the main repository's root. A .git
// file without a common directory (a submodule) leaves dir as the root.
func worktreeMain(dir, gitFile string) string {
	b, err := os.ReadFile(gitFile)
	if err != nil {
		return dir
	}
	gitdir, ok := strings.CutPrefix(strings.TrimSpace(string(b)), "gitdir:")
	if !ok {
		return dir
	}
	gitdir = strings.TrimSpace(gitdir)
	if !filepath.IsAbs(gitdir) {
		gitdir = filepath.Join(dir, gitdir)
	}
	common, err := os.ReadFile(filepath.Join(gitdir, "commondir"))
	if err != nil {
		return dir
	}
	c := strings.TrimSpace(string(common))
	if !filepath.IsAbs(c) {
		c = filepath.Join(gitdir, c)
	}
	c = filepath.Clean(c)
	if real, err := filepath.EvalSymlinks(c); err == nil {
		c = real
	}
	if filepath.Base(c) != ".git" {
		return dir // a bare repository: no main folder to name it after
	}
	return filepath.Dir(c)
}

func newID() (string, error) {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// forFolder returns the Project for folder, recognising it if new.
func (s *projects) forFolder(folder string) (Project, error) {
	root := repositoryRoot(folder)
	s.mu.Lock()
	defer s.mu.Unlock()
	var body string
	err := s.db.QueryRow(`SELECT body FROM projects WHERE root = ?`, root).Scan(&body)
	if err == nil {
		var p Project
		return p, json.Unmarshal([]byte(body), &p)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Project{}, err
	}
	all, err := s.list()
	if err != nil {
		return Project{}, err
	}
	id, err := newID()
	if err != nil {
		return Project{}, err
	}
	now := time.Now().UTC()
	p := Project{ID: id, Root: root, Name: filepath.Base(root), Colour: leastUsedColour(all), CreatedAt: now, UpdatedAt: now}
	p.GitRemote = gitRemote(root)
	p.Key = keyFor(p)
	// The same repository on another computer is the same Project: it
	// takes that Project's name, colour and logo.
	if s.pings.set != nil {
		if sp, ok := s.pings.set.sharedByKey(p.Key); ok {
			s.adoptShared(&p, sp)
		}
	}
	return p, s.save(p, true)
}

func leastUsedColour(all []Project) string {
	used := map[string]int{}
	for _, p := range all {
		used[strings.ToUpper(p.Colour)]++
	}
	best := Palette[0]
	for _, c := range Palette {
		if used[c] < used[best] {
			best = c
		}
	}
	return best
}

func (s *projects) save(p Project, insert bool) error {
	body, _ := json.Marshal(p)
	var err error
	if insert {
		_, err = s.db.Exec(`INSERT INTO projects (id, root, body) VALUES (?,?,?)`, p.ID, p.Root, string(body))
	} else {
		_, err = s.db.Exec(`UPDATE projects SET body = ? WHERE id = ?`, string(body), p.ID)
	}
	if err != nil {
		return err
	}
	if s.pings.set != nil {
		s.pings.set.projectChanged(p, s.logo(p))
	}
	return s.pings.appendEvent(EventProjectChanged, p.UpdatedAt, p)
}

func (s *projects) get(id string) (Project, error) {
	var p Project
	var body string
	err := s.db.QueryRow(`SELECT body FROM projects WHERE id = ?`, id).Scan(&body)
	if errors.Is(err, sql.ErrNoRows) {
		return p, errNotFound
	}
	if err != nil {
		return p, err
	}
	return p, json.Unmarshal([]byte(body), &p)
}

func (s *projects) list() ([]Project, error) {
	rows, err := s.db.Query(`SELECT body FROM projects ORDER BY rowid`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Project{}
	for rows.Next() {
		var body string
		var p Project
		if err := rows.Scan(&body); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(body), &p); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// update applies change to the Project with id and saves it.
func (s *projects) update(id string, change func(*Project) error) (Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := s.get(id)
	if err != nil {
		return p, err
	}
	before := p
	if err := change(&p); err != nil {
		return p, err
	}
	if sameApartFromTime(before, p, func(x *Project) { x.UpdatedAt = time.Time{} }) {
		return before, nil // nothing to write or tell
	}
	p.UpdatedAt = time.Now().UTC()
	return p, s.save(p, false)
}

// place sets a new Ping's Project from its folder, or its Session's, and
// registers the Session if it is not yet known.
func (s *projects) place(p *Ping) error {
	if p.Harness == "" {
		// Sent while its Harness's hook could not reach the Engine (it was
		// restarting): the Ping belongs to the Session known by its id.
		h, err := s.onlyHarnessOf(p.Session)
		if err != nil {
			return err
		}
		p.Harness = h
	}
	sess, err := s.sessionForPing(*p)
	if err != nil {
		return err
	}
	folder := p.Folder
	if folder == "" {
		folder = sess.Folder
	}
	if folder == "" {
		return nil
	}
	proj, err := s.forFolder(folder)
	if err != nil {
		return err
	}
	p.Project = proj.ID
	return s.keepTagColours(proj.ID, p.Tags)
}

// keepTagColours gives each Tag the colour its text was first given in the
// Project, recording the colour when this is the first. Text is compared
// without regard to case; a text never given a colour stays neutral. Only a
// pastel from tagicons.Colours, in any case, counts as a colour, and is kept
// lower-cased; a Tag with any other colour is treated as having none.
//
// It rewrites the incoming Ping when it is stored, not every Ping when it is
// read: reads stay plain, and a Ping stored before its Tag's text was given a
// colour keeps what it was stored with.
func (s *projects) keepTagColours(project string, tags []Tag) error {
	for i, t := range tags {
		key := strings.ToLower(t.Text)
		if c := strings.ToLower(t.Colour); slices.Contains(tagicons.Colours, c) {
			if _, err := s.db.Exec(`INSERT OR IGNORE INTO tag_colours (project, text, colour) VALUES (?,?,?)`, project, key, c); err != nil {
				return err
			}
		}
		var colour string
		err := s.db.QueryRow(`SELECT colour FROM tag_colours WHERE project = ? AND text = ?`, project, key).Scan(&colour)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		tags[i].Colour = colour
	}
	return nil
}
