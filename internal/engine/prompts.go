package engine

import (
	"net/http"
	"sort"
	"strings"
)

// RecentPromptsLimit is how many first prompts GET /prompts gives.
const RecentPromptsLimit = 20

// RecentPrompts is the answer to GET /prompts: the first prompts of a
// Project's Sessions, newest first, each once.
type RecentPrompts struct {
	Prompts []string `json:"prompts"`
}

func (s *projects) promptRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /prompts", func(w http.ResponseWriter, r *http.Request) {
		project := r.URL.Query().Get("project")
		if project == "" {
			writeError(w, http.StatusBadRequest, "say which Project: ?project=")
			return
		}
		out, err := s.recentPrompts(project, RecentPromptsLimit)
		writeSession(w, RecentPrompts{Prompts: out}, err)
	})
}

// recentPrompts gives the first prompts of the Project's Sessions, newest
// Session first, from any Harness and however they started. A prompt given to
// several Sessions is listed once, where it was newest.
func (s *projects) recentPrompts(project string, limit int) ([]string, error) {
	all, err := s.sessions()
	if err != nil {
		return nil, err
	}
	// Newest recorded first, so Sessions created in the same instant keep that order.
	for i, j := 0, len(all)-1; i < j; i, j = i+1, j-1 {
		all[i], all[j] = all[j], all[i]
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].CreatedAt.After(all[j].CreatedAt) })
	out := []string{}
	seen := map[string]bool{}
	for _, x := range all {
		p := strings.TrimSpace(x.FirstPrompt)
		if x.Project != project || p == "" || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
		if len(out) == limit {
			break
		}
	}
	return out, nil
}
