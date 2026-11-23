package engine

import (
	"maps"
	"net/http"
	"slices"
	"sort"
	"sync"
)

// SkillReader names the Skills a Harness offers the human: user ones from
// their home folder, project ones from the Project at root. The command that
// runs the Engine supplies it with the Harnesses; nil means none are named.
type SkillReader interface {
	Skills(harness, root string) (user, project []string)
}

// Skills is the answer to GET /skills: every Skill name the Harness offers in
// the Project or folder, sorted.
type Skills struct {
	Skills []string `json:"skills"`
}

// skillNames keeps the names of each Harness's Skills on this computer. They
// are read from disk only when an interface asks for them, when a Session
// starts in a Project, and once as the Engine starts; never on a timer.
type skillNames struct {
	r  SkillReader
	pr *projects

	mu      sync.Mutex
	user    map[string][]string            // harness → names
	project map[string]map[string][]string // harness → Project key → names
	seen    map[SessionRef]bool            // Sessions whose Project was read as they started

	// changed reports this computer's state again; set once it shares a set.
	changed func()
	// remote answers for a Project on another computer; set once it shares a set.
	remote func(harness, project, computer string) ([]string, bool)
}

func newSkillNames(r SkillReader, pr *projects) *skillNames {
	return &skillNames{r: r, pr: pr, user: map[string][]string{}, project: map[string]map[string][]string{}, seen: map[SessionRef]bool{}}
}

// read reads a Harness's Skills for the folder at root, a Project's when key
// is not "", keeps them, and gives every name that applies there.
func (s *skillNames) read(harness, root, key string) []string {
	if s.r == nil {
		return []string{}
	}
	user, project := s.r.Skills(harness, root)
	user, project = sortedNames(user), sortedNames(project)
	s.mu.Lock()
	changed := !slices.Equal(s.user[harness], user)
	s.user[harness] = user
	if key != "" {
		if s.project[harness] == nil {
			s.project[harness] = map[string][]string{}
		}
		old, had := s.project[harness][key]
		changed = changed || !slices.Equal(old, project) || !had && len(project) > 0
		if len(project) == 0 {
			delete(s.project[harness], key)
		} else {
			s.project[harness][key] = project
		}
	}
	report := s.changed
	s.mu.Unlock()
	if changed && report != nil {
		go report()
	}
	return sortedNames(append(slices.Clone(user), project...))
}

// readAll reads every Harness's Skills for every Project here, as the Engine
// starts.
func (s *skillNames) readAll(harnesses []string) {
	all, _ := s.pr.list()
	for _, h := range harnesses {
		s.read(h, "", "")
		for _, p := range all {
			s.read(h, p.Root, p.Key)
		}
	}
}

// sessionSeen reads the Skills of the Project a Session starts in, once per
// Session.
func (s *skillNames) sessionSeen(sess Session) {
	ref := SessionRef{Harness: sess.Harness, ID: sess.ID}
	s.mu.Lock()
	seen := s.seen[ref]
	s.seen[ref] = true
	s.mu.Unlock()
	if seen || s.r == nil {
		return
	}
	root, key := sess.Folder, ""
	if p, err := s.pr.get(sess.Project); err == nil {
		root, key = p.Root, p.Key
	}
	if root != "" {
		go s.read(sess.Harness, root, key)
	}
}

// of gives the names kept for a Harness, for this computer's state: those
// from the human's home folder, and each Project's own by key.
func (s *skillNames) of(harness string) (user []string, project map[string][]string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.user[harness], maps.Clone(s.project[harness])
}

func (s *skillNames) routes(mux *http.ServeMux, thisComputer func() string) {
	mux.HandleFunc("GET /skills", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		harness, project, folder, computer := q.Get("harness"), q.Get("project"), q.Get("folder"), q.Get("computer")
		if harness == "" || (project == "") == (folder == "") {
			writeError(w, http.StatusBadRequest, "say which Harness, and a project or a folder: ?harness=&project= or &folder=")
			return
		}
		here := computer == "" || computer == thisComputer()
		switch {
		case folder != "" && here:
			writeJSON(w, http.StatusOK, Skills{s.read(harness, folder, "")})
			return
		case project != "" && here:
			if p, err := s.pr.get(project); err == nil {
				writeJSON(w, http.StatusOK, Skills{s.read(harness, p.Root, p.Key)})
				return
			}
		}
		if s.remote != nil && project != "" {
			if names, ok := s.remote(harness, project, computer); ok {
				writeJSON(w, http.StatusOK, Skills{names})
				return
			}
		}
		// Nothing known there yet: no names, so nothing is offered.
		writeJSON(w, http.StatusOK, Skills{[]string{}})
	})
}

func sortedNames(names []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, n := range names {
		if n != "" && !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out
}
