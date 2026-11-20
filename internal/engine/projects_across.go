package engine

import (
	"bytes"
	"encoding/json"
	"errors"
	"image/png"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/jacksonlw/pong/wire"
)

// sharedProject is the Project metadata one computer sends the others: the
// only thing copied between Devices.
type sharedProject struct {
	Key      string    `json:"key"`
	ID       string    `json:"id"` // the sender's id for it
	Name     string    `json:"name"`
	Colour   string    `json:"colour"`
	Logo     []byte    `json:"logo,omitempty"`
	HasLogo  bool      `json:"has_logo"`
	EditedAt time.Time `json:"edited_at"`
	Enabled  bool      `json:"enabled"` // on the sender
	// LogoEdit, in an edit of a Project the sender does not have (no id),
	// says what the edit does to the logo: logoSet with Logo, logoRemove, or
	// nothing.
	LogoEdit string `json:"logo_edit,omitempty"`
}

const (
	logoSet    = "set"
	logoRemove = "remove"
)

// An edit carries its logo inside one sealed message, which the relay takes
// up to 256 KiB of: a PNG of at most editLogoSide pixels on its longer side
// and editLogoBytes, which fits once base64-encoded.
const (
	editLogoSide  = 256
	editLogoBytes = 144 * 1024
)

// editLogoOK reports whether b is a logo an edit may carry.
func editLogoOK(b []byte) bool {
	if len(b) == 0 || len(b) > editLogoBytes || http.DetectContentType(b) != "image/png" {
		return false
	}
	c, err := png.DecodeConfig(bytes.NewReader(b))
	return err == nil && c.Width > 0 && c.Height > 0 && c.Width <= editLogoSide && c.Height <= editLogoSide
}

// gitRemote is the normalised address of the repository's origin remote, or
// "" for a plain directory or a repository with none.
func gitRemote(root string) string {
	out, err := exec.Command("git", "-C", root, "config", "--get", "remote.origin.url").Output()
	if err != nil {
		return ""
	}
	return normaliseRemote(strings.TrimSpace(string(out)))
}

var (
	scpLike = regexp.MustCompile(`^(?:[^@/]+@)?([^:/]+):(.+)$`)
	scpPort = regexp.MustCompile(`^\d+/`)
)

// normaliseRemote writes one repository address one way, so that the ways
// of writing it are equal: no scheme, user or port, host in lower case, no
// .git suffix or trailing slash. git@github.com:me/app.git,
// https://github.com/me/app and ssh://git@github.com:22/me/app/ are all
// github.com/me/app.
func normaliseRemote(r string) string {
	r = strings.TrimSpace(r)
	if r == "" {
		return ""
	}
	var host, path string
	if u, err := url.Parse(r); err == nil && u.Scheme != "" {
		if u.Scheme == "file" || u.Host == "" {
			return "" // a path on this computer says nothing about another's
		}
		host, path = u.Hostname(), u.Path
	} else if m := scpLike.FindStringSubmatch(r); m != nil && !strings.HasPrefix(r, "/") && !strings.HasPrefix(r, ".") {
		host, path = m[1], scpPort.ReplaceAllString(m[2], "")
	} else {
		return "" // a local path
	}
	path = strings.Trim(path, "/")
	path = strings.TrimSuffix(path, ".git")
	if path == "" {
		return ""
	}
	return strings.ToLower(host) + "/" + path
}

// keyFor is a Project's key when it is first recognised.
func keyFor(p Project) string {
	if p.GitRemote != "" {
		return "git:" + p.GitRemote
	}
	return "id:" + p.ID
}

// keyAll gives Projects recognised before Projects had keys their key.
func (s *projects) keyAll() error {
	all, err := s.list()
	if err != nil {
		return err
	}
	for _, p := range all {
		if p.Key != "" {
			continue
		}
		p.GitRemote = gitRemote(p.Root)
		p.Key = keyFor(p)
		p.EditedAt = p.UpdatedAt
		body, _ := json.Marshal(p)
		if _, err := s.db.Exec(`UPDATE projects SET body = ? WHERE id = ?`, string(body), p.ID); err != nil {
			return err
		}
	}
	return nil
}

// logo returns the Project's logo, or nil.
func (s *projects) logo(p Project) []byte {
	if p.Logo == "" {
		return nil
	}
	b, _ := os.ReadFile(filepath.Join(s.logos, p.ID))
	return b
}

// adoptShared gives a local Project another computer's name, colour and logo.
// A logo said to exist but not sent is not taken, nor is anything else.
func (s *projects) adoptShared(p *Project, sp sharedProject) {
	if sp.HasLogo && len(sp.Logo) == 0 {
		return
	}
	p.Name, p.Colour, p.EditedAt = sp.Name, sp.Colour, sp.EditedAt
	if sp.HasLogo && len(sp.Logo) > 0 && os.MkdirAll(s.logos, 0o700) == nil &&
		os.WriteFile(filepath.Join(s.logos, p.ID), sp.Logo, 0o600) == nil {
		p.Logo = "/projects/" + p.ID + "/logo?v=" + sp.EditedAt.Format("20060102150405.000000000")
	} else if !sp.HasLogo {
		os.Remove(filepath.Join(s.logos, p.ID))
		p.Logo = ""
	}
}

// link makes the local Project id the same Project as to, which may be on
// another computer: it takes that Project's key, name, colour and logo.
func (s *projects) link(id, to string) (Project, error) {
	var sp sharedProject
	if t, err := s.get(to); err == nil {
		sp = sharedProject{Key: t.Key, Name: t.Name, Colour: t.Colour, EditedAt: t.EditedAt, HasLogo: t.Logo != "", Logo: s.logo(t)}
	} else if s.pings.set != nil {
		var ok bool
		if sp, ok = s.pings.set.sharedByID(to); !ok {
			return Project{}, errNotFound
		}
	} else {
		return Project{}, errNotFound
	}
	if id == to {
		return Project{}, errState{"a Project cannot be linked to itself"}
	}
	return s.update(id, func(p *Project) error {
		p.Key = sp.Key
		s.adoptShared(p, sp)
		if p.EditedAt.IsZero() {
			p.EditedAt = time.Now().UTC()
		}
		return nil
	})
}

func (s *projects) writeProject(w http.ResponseWriter, p Project, err error) {
	if err == nil && s.pings.set != nil && !p.Remote {
		s.pings.set.decorateProject(&p)
	}
	var st errState
	if errors.As(err, &st) {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeProject(w, p, err)
}

// projectChanged sends a local Project's metadata to every Device.
func (a *across) projectChanged(p Project, logo []byte) {
	sp := sharedProject{Key: p.Key, ID: p.ID, Name: p.Name, Colour: p.Colour, EditedAt: p.EditedAt, Enabled: p.Enabled, HasLogo: p.Logo != "", Logo: logo}
	b, _ := json.Marshal(sp)
	a.qmu.Lock()
	a.queue = append(a.queue, &wire.Project{Project: b})
	a.qmu.Unlock()
	select {
	case a.wake <- struct{}{}:
	default:
	}
	a.mu.Lock()
	f := a.stateChanged
	a.mu.Unlock()
	if f != nil {
		go f() // whether it is enabled here is part of this computer's state
	}
}

// sendProjects sends every local Project to a Device that just connected.
func (a *across) sendProjects(to string) {
	if a.pr == nil {
		return
	}
	all, err := a.pr.list()
	if err != nil {
		return
	}
	for _, p := range all {
		sp := sharedProject{Key: p.Key, ID: p.ID, Name: p.Name, Colour: p.Colour, EditedAt: p.EditedAt, Enabled: p.Enabled, HasLogo: p.Logo != "", Logo: a.pr.logo(p)}
		b, _ := json.Marshal(sp)
		a.node.Send(to, &wire.Project{Project: b})
	}
}

// onProject keeps another computer's Project metadata in memory and, when
// this computer has the same Project and the edit is newer, takes it.
func (a *across) onProject(from string, m wire.Message) {
	var sp sharedProject
	if json.Unmarshal(m.(*wire.Project).Project, &sp) != nil || sp.Key == "" {
		return
	}
	// A phone holds no Projects of its own, so from it only an edit (no id)
	// is taken; computers send their Projects whole.
	if !a.fromComputer(from) && sp.ID != "" {
		return
	}
	if sp.ID == "" {
		// An edit changes the name and colour, and the logo only when it
		// says so; whether a Project is enabled is each computer's own.
		sp.Enabled = false
		switch sp.LogoEdit {
		case logoSet:
			if !editLogoOK(sp.Logo) {
				return
			}
			sp.HasLogo = true
		case "", logoRemove:
			sp.Logo, sp.HasLogo = nil, false
		default:
			return
		}
	} else {
		sp.LogoEdit = ""
	}
	a.mu.Lock()
	var old sharedProject
	had := false
	if sp.ID == "" {
		// An edit of a Project the sender does not have: it changes the
		// metadata every computer keeps for the key, nothing else.
		for comp, ps := range a.shared {
			if cur, ok := ps[sp.Key]; ok && sp.EditedAt.After(cur.EditedAt) {
				cur.Name, cur.Colour, cur.EditedAt = sp.Name, sp.Colour, sp.EditedAt
				if sp.LogoEdit != "" {
					cur.Logo, cur.HasLogo = sp.Logo, sp.HasLogo
				}
				a.shared[comp][sp.Key] = cur
			}
		}
	} else {
		if a.shared[from] == nil {
			a.shared[from] = map[string]sharedProject{}
		}
		for k, o := range a.shared[from] {
			if o.ID == sp.ID && k != sp.Key {
				delete(a.shared[from], k) // linked since: its old key is gone
			}
		}
		old, had = a.shared[from][sp.Key]
		a.shared[from][sp.Key] = sp
	}
	a.mu.Unlock()
	if a.pr == nil {
		return
	}
	if local, err := a.pr.byKey(sp.Key); err == nil {
		if sp.EditedAt.After(local.EditedAt) {
			a.pr.update(local.ID, func(p *Project) error {
				switch {
				case !sp.EditedAt.After(p.EditedAt): // decided under the Project's lock
				case sp.ID == "" && sp.LogoEdit == "":
					p.Name, p.Colour, p.EditedAt = sp.Name, sp.Colour, sp.EditedAt
				default:
					a.pr.adoptShared(p, sp)
				}
				return nil
			})
		} else if !had || old.Enabled != sp.Enabled {
			a.s.appendMem(EventProjectChanged, a.s.clock(), a.decorated(local))
		}
		return
	}
	if sp.ID == "" {
		if latest, ok := a.sharedByKey(sp.Key); ok {
			sp.ID = latest.ID
		}
	}
	if p, err := a.remoteProject(sp.ID); err == nil {
		a.s.appendMem(EventProjectChanged, a.s.clock(), p)
	}
}

func (a *across) decorated(p Project) Project {
	a.decorateProject(&p)
	return p
}

// byKey returns the local Project with key.
func (s *projects) byKey(key string) (Project, error) {
	all, err := s.list()
	if err != nil {
		return Project{}, err
	}
	for _, p := range all {
		if p.Key == key {
			return p, nil
		}
	}
	return Project{}, errNotFound
}

// sharedByKey returns the latest metadata another computer sent for key.
func (a *across) sharedByKey(key string) (sharedProject, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	var best sharedProject
	ok := false
	for _, ps := range a.shared {
		if sp, has := ps[key]; has && (!ok || sp.EditedAt.After(best.EditedAt)) {
			best, ok = sp, true
		}
	}
	return best, ok
}

// sharedByID returns another computer's Project by that computer's id.
func (a *across) sharedByID(id string) (sharedProject, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, ps := range a.shared {
		for _, sp := range ps {
			if sp.ID == id {
				return sp, true
			}
		}
	}
	return sharedProject{}, false
}

// decorateProject names the computers that take Sessions in a local Project.
func (a *across) decorateProject(p *Project) {
	var on []string
	if p.Enabled {
		on = append(on, a.selfName())
	}
	a.mu.Lock()
	for comp, ps := range a.shared {
		if sp, ok := ps[p.Key]; ok && sp.Enabled {
			d, _, _ := a.node.Member(comp)
			on = append(on, d.Name)
		}
	}
	a.mu.Unlock()
	p.EnabledOn = on
}

func (a *across) selfName() string {
	d, _, _ := a.node.Member(a.node.ID())
	return d.Name
}

// remoteProjects are Projects only other computers have, one per key.
func (a *across) remoteProjects(local []Project) []Project {
	have := map[string]bool{}
	for _, p := range local {
		have[p.Key] = true
	}
	a.mu.Lock()
	keys := map[string]bool{}
	for _, ps := range a.shared {
		for k := range ps {
			keys[k] = !have[k]
		}
	}
	a.mu.Unlock()
	var out []Project
	for k, only := range keys {
		if !only {
			continue
		}
		sp, _ := a.sharedByKey(k)
		if p, err := a.remoteProject(sp.ID); err == nil {
			out = append(out, p)
		}
	}
	return out
}

// remoteProject shows another computer's Project, by that computer's id.
func (a *across) remoteProject(id string) (Project, error) {
	sp, ok := a.sharedByID(id)
	if !ok {
		return Project{}, errNotFound
	}
	latest, _ := a.sharedByKey(sp.Key)
	p := Project{ID: id, Key: sp.Key, Name: latest.Name, Colour: latest.Colour, EditedAt: latest.EditedAt, Remote: true}
	if latest.HasLogo {
		p.Logo = "/projects/" + id + "/logo?v=" + latest.EditedAt.Format("20060102150405.000000000")
	}
	a.decorateProject(&p)
	return p, nil
}

func (a *across) isRemoteProject(id string) bool {
	if a.pr != nil {
		if _, err := a.pr.get(id); err == nil {
			return false
		}
	}
	_, ok := a.sharedByID(id)
	return ok
}

// editRemoteProject edits a Project only other computers have: the edit
// goes to every Device, and the computers that have it take it.
func (a *across) editRemoteProject(id string, name, colour *string) (Project, error) {
	sp, ok := a.sharedByID(id)
	if !ok {
		return Project{}, errNotFound
	}
	sp, _ = a.sharedByKey(sp.Key)
	if name != nil {
		sp.Name = strings.TrimSpace(*name)
	}
	if colour != nil {
		sp.Colour = strings.ToUpper(*colour)
	}
	sp.EditedAt = time.Now().UTC()
	out := sp
	out.ID, out.Enabled, out.Logo = "", false, nil // not this computer's
	b, _ := json.Marshal(out)
	a.node.Broadcast(&wire.Project{Project: b}, "")
	a.mu.Lock()
	for comp, ps := range a.shared {
		if old, has := ps[sp.Key]; has {
			sp.ID, sp.Enabled = old.ID, old.Enabled
			ps[sp.Key] = sp
			a.shared[comp] = ps
		}
	}
	a.mu.Unlock()
	return a.remoteProject(id)
}

// remoteLogo returns the logo of a Project only other computers have.
func (a *across) remoteLogo(id string) ([]byte, bool) {
	sp, ok := a.sharedByID(id)
	if !ok {
		return nil, false
	}
	latest, _ := a.sharedByKey(sp.Key)
	return latest.Logo, latest.HasLogo && len(latest.Logo) > 0
}

// localProjectFor is the local id of the Project another computer calls id,
// when this computer has it too.
func (a *across) localProjectFor(id string) (string, bool) {
	sp, ok := a.sharedByID(id)
	if !ok || a.pr == nil {
		return "", false
	}
	p, err := a.pr.byKey(sp.Key)
	return p.ID, err == nil
}
