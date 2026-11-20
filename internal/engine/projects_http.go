package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// ChangeProject is the body of PATCH /projects/{id}; absent fields stay.
type ChangeProject struct {
	Name   *string `json:"name,omitempty"`
	Colour *string `json:"colour,omitempty"`
	// Enabled lets this computer take Sessions started from other Devices
	// in the Project. It is not an edit of the Project and stays here.
	Enabled *bool `json:"enabled,omitempty"`
}

var hexColour = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)

// maxLogo is the largest logo file the Engine keeps.
const maxLogo = 1 << 20

func (s *projects) routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /projects", func(w http.ResponseWriter, r *http.Request) {
		if f := r.URL.Query().Get("folder"); f != "" {
			// The Project for a folder on this computer, recognised if new.
			p, err := s.forFolder(f)
			s.writeProject(w, p, err)
			return
		}
		ps, err := s.list()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if set := s.pings.set; set != nil {
			for i := range ps {
				set.decorateProject(&ps[i])
			}
			ps = append(ps, set.remoteProjects(ps)...)
		}
		writeJSON(w, http.StatusOK, ps)
	})
	mux.HandleFunc("GET /projects/{id}", func(w http.ResponseWriter, r *http.Request) {
		p, err := s.get(r.PathValue("id"))
		if errors.Is(err, errNotFound) && s.pings.set != nil {
			p, err = s.pings.set.remoteProject(r.PathValue("id"))
		}
		s.writeProject(w, p, err)
	})
	mux.HandleFunc("POST /projects/{id}/link", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			To string `json:"to"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.To == "" {
			writeError(w, http.StatusBadRequest, "to is required: the id of the Project to link this one to")
			return
		}
		p, err := s.link(r.PathValue("id"), in.To)
		s.writeProject(w, p, err)
	})
	mux.HandleFunc("PATCH /projects/{id}", func(w http.ResponseWriter, r *http.Request) {
		var in ChangeProject
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, "body is not a change to a Project: "+err.Error())
			return
		}
		if in.Name != nil && strings.TrimSpace(*in.Name) == "" {
			writeError(w, http.StatusBadRequest, "name cannot be empty: give the Project a name, or leave name out")
			return
		}
		if in.Colour != nil && !hexColour.MatchString(*in.Colour) {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("colour %q is not a colour: use #RRGGBB", *in.Colour))
			return
		}
		if set := s.pings.set; set != nil && set.isRemoteProject(r.PathValue("id")) {
			if in.Enabled != nil {
				writeError(w, http.StatusConflict, "this Project is not on this computer: enable it on a computer it is on")
				return
			}
			p, err := set.editRemoteProject(r.PathValue("id"), in.Name, in.Colour)
			s.writeProject(w, p, err)
			return
		}
		p, err := s.update(r.PathValue("id"), func(p *Project) error {
			if in.Name != nil || in.Colour != nil {
				p.EditedAt = time.Now().UTC()
			}
			if in.Name != nil {
				p.Name = strings.TrimSpace(*in.Name)
			}
			if in.Colour != nil {
				p.Colour = strings.ToUpper(*in.Colour)
			}
			if in.Enabled != nil {
				p.Enabled = *in.Enabled
			}
			return nil
		})
		s.writeProject(w, p, err)
	})
	mux.HandleFunc("PUT /projects/{id}/logo", func(w http.ResponseWriter, r *http.Request) {
		b, err := io.ReadAll(io.LimitReader(r.Body, maxLogo+1))
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		ct := http.DetectContentType(b)
		switch {
		case len(b) > maxLogo:
			writeError(w, http.StatusBadRequest, "the logo is over 1 MB: send a smaller image")
			return
		case ct != "image/png" && ct != "image/jpeg":
			writeError(w, http.StatusBadRequest, "the logo is not a PNG or JPEG image: send the image file as the body")
			return
		}
		p, err := s.update(r.PathValue("id"), func(p *Project) error {
			if err := os.MkdirAll(s.logos, 0o700); err != nil {
				return err
			}
			if err := os.WriteFile(filepath.Join(s.logos, p.ID), b, 0o600); err != nil {
				return err
			}
			p.Logo = fmt.Sprintf("/projects/%s/logo?v=%d", p.ID, time.Now().UnixNano())
			p.EditedAt = time.Now().UTC()
			return nil
		})
		s.writeProject(w, p, err)
	})
	mux.HandleFunc("DELETE /projects/{id}/logo", func(w http.ResponseWriter, r *http.Request) {
		p, err := s.update(r.PathValue("id"), func(p *Project) error {
			if err := os.Remove(filepath.Join(s.logos, p.ID)); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			p.Logo = ""
			p.EditedAt = time.Now().UTC()
			return nil
		})
		s.writeProject(w, p, err)
	})
	mux.HandleFunc("GET /projects/{id}/logo", func(w http.ResponseWriter, r *http.Request) {
		if set := s.pings.set; set != nil && set.isRemoteProject(r.PathValue("id")) {
			b, ok := set.remoteLogo(r.PathValue("id"))
			if !ok {
				writeProject(w, Project{}, errNotFound)
				return
			}
			w.Header().Set("Content-Type", http.DetectContentType(b))
			w.Write(b)
			return
		}
		p, err := s.get(r.PathValue("id"))
		if err == nil && p.Logo == "" {
			err = errNotFound
		}
		if err != nil {
			writeProject(w, p, err)
			return
		}
		http.ServeFile(w, r, filepath.Join(s.logos, p.ID))
	})
	s.sessionRoutes(mux)
}

func writeProject(w http.ResponseWriter, p Project, err error) {
	switch {
	case errors.Is(err, errNotFound):
		writeError(w, http.StatusNotFound, "no such Project, or it has no logo: check the id")
	case err != nil:
		writeError(w, http.StatusInternalServerError, err.Error())
	default:
		writeJSON(w, http.StatusOK, p)
	}
}
